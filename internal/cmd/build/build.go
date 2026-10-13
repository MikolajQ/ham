package build

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/antony-jr/ham/internal/banner"
	"github.com/antony-jr/ham/internal/core"
	"github.com/antony-jr/ham/internal/helpers"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"

	"github.com/mkideal/cli"
	"github.com/sevlyar/go-daemon"
)

type buildT struct {
	cli.Helper
	Sum        string `cli:"*s,sum" usage:"SHA256 Hash of the main ham.yaml file"`
	RecipePath string `cli:"*r,recipe" usage:"Recipe file path which has the ham.yaml"`
	VarsPath   string `cli:"*a,vars" usage:"JSON file path containing all required build variables prompted"`
	KeepServer bool   `cli:"k,keep-server" usage:"Don't Destroy the Remote Server on any error."`
	FromStep   int    `cli:"f,from-step" usage:"Skip build entries before this index (resume a kept, failed build; see /tmp/ham-build.result)" dft:"0"`
	// The server name and its self-destroy derive from the recipe sum, so a
	// changed ham.yml used to mean a new server. With this flag a kept server
	// runs the changed recipe under the identity of --sum.
	AcceptRecipeChange bool `cli:"accept-recipe-change" usage:"Run a changed ham.yml on a kept server; the build keeps the identity (server name, self-destroy) of --sum"`
}

// Outcome of the last run for scripts on the build server (resume, monitoring):
// "running", "success" or "failed <build entry index>" (-1 = before the build entries,
// len(build) = post_build).
const ResultFile = "/tmp/ham-build.result"

func writeResult(result string) {
	_ = os.WriteFile(ResultFile, []byte(result+"\n"), 0644)
}

type statusT struct {
	Quit       bool
	Status     string
	Title      string
	Error      error
	Percentage int
}

func NewCommand() *cli.Command {
	return &cli.Command{
		Name: "build",
		Desc: "Build ASOP from source from a recipe and given build vars. (*Run in Build Machine) (Private)",
		Argv: func() interface{} { return new(buildT) },
		Fn: func(ctx *cli.Context) error {
			argv := ctx.Argv().(*buildT)
			if runtime.GOOS != "linux" {
				return errors.New("OS Not Supported.")
			}

			hf, err := core.NewHAMFile(argv.RecipePath)
			if err != nil {
				return err
			}

			banner.BuildStartBanner()

			fmt.Printf("%s\n", hf.Title)
			fmt.Printf("v%s\n", hf.Version)
			fmt.Printf("SHA256 Sum: %s\n", hf.SHA256Sum)

			buildID := hf.SHA256Sum
			if hf.SHA256Sum != argv.Sum {
				if !argv.AcceptRecipeChange {
					return errors.New("SHA256 Mismatch, Bad File.")
				}
				fmt.Printf("ham.yml changed, keeping the build identity %s\n", argv.Sum)
				buildID = argv.Sum
			}

			// We assume the current server name at hetzner to
			// be this and we use this assumption to destroy
			// the server when the build is done.
			serverName := helpers.ServerNameFromSHA256(buildID)
			fmt.Printf("Build Server: %s\n", serverName)

			// len(build) = only post_build (e.g. a failed upload).
			if argv.FromStep < 0 || argv.FromStep > len(hf.Build) {
				return fmt.Errorf("--from-step %d out of range (0..%d)", argv.FromStep, len(hf.Build))
			}
			dctx := &daemon.Context{
				PidFileName: "/tmp/com.github.antony-jr.ham.pid",
				PidFilePerm: 0644,
				LogFileName: "/tmp/com.github.antony-jr.ham.log",
				LogFilePerm: 0640,
				WorkDir:     "./",
				Umask:       027,
			}
			dctx.Args = []string{"ham", "build",
				"-r", argv.RecipePath,
				"-a", argv.VarsPath,
				"-s", argv.Sum,
				"-f", strconv.Itoa(argv.FromStep)}
			if argv.KeepServer {
				dctx.Args = append(dctx.Args, "--keep-server")
			}
			if argv.AcceptRecipeChange {
				dctx.Args = append(dctx.Args, "--accept-recipe-change")
			}

			d, err := dctx.Reborn()
			if err != nil {
				return err
			}

			if d != nil {
				banner.BuildFinishBanner()
				return nil
			}

			// Daemon Execution
			// Actual Builder

			// This holds the status in json,
			// the TCP server responds with this
			// status string when asked
			status := statusT{
				false,
				"Running",
				"",
				nil,
				0,
			}

			go statusServer(&status)

			config, err := core.GetConfiguration()
			if err != nil {
				return checkErrorStatus(&status, err)
			}
			client := hcloud.NewClient(hcloud.WithToken(config.APIKey))

			// A successful build always destroys its server and
			// volume from here, so nothing depends on the client
			// staying alive. --keep-server only keeps a failed build
			// around for debugging.
			succeeded := false
			failedStep := -1
			writeResult("running")
			defer func() {
				if succeeded {
					writeResult("success")
				} else {
					writeResult(fmt.Sprintf("failed %d", failedStep))
					runOnFailure(hf.OnFailure, buildID, failedStep, entryTitle(hf.Build, failedStep))
					// A failed daemon used to stay alive after printing its error, which
					// blocked a resume and hid the failure from monitoring. Whatever hangs
					// after this point, the process ends.
					time.AfterFunc(3*time.Minute, func() {
						fmt.Println("Exit watchdog: build failed, process still alive, exiting.")
						os.Exit(1)
					})
				}
				if succeeded || !argv.KeepServer {
					destroyCurrentServer(client, buildID)
					return
				}
				scheduleFailedServerCleanup(buildID)
			}()

			// A new run on a kept server cancels the cleanup left by
			// the failed one.
			_ = exec.Command("systemctl", "stop", FailedServerUnit+".timer", FailedServerUnit+".service").Run()

			vars, err := helpers.ReadVarsJsonFile(argv.VarsPath)
			if err != nil {
				return checkErrorStatus(&status, err)
			}

			// Get ham ssh key
			hamSSHKey, _, err := client.SSHKey.Get(
				context.Background(),
				"ham-ssh-key",
			)
			if err != nil {
				return checkErrorStatus(&status, err)
			}

			// Set Label to Indicate Progress of
			// this build.
			hamSSHKey, err = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "inprogress")
			if err != nil {
				return checkErrorStatus(&status, err)
			}

			// Install Dependencies for LineageOS build/AOSP
			{
				term, err := NewTerminal(buildID + "-prebuild")
				if err != nil {
					hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
					return checkErrorStatus(&status, err)
				}

				// Ubuntu 24.04 LTS on the Hetzner VM. Names match noble:
				// libncurses5* are gone, and current LineageOS needs erofs,
				// protobuf and xxd on top of the old host package list.
				deps := []string{
					"bc",
					"bison",
					"build-essential",
					"curl",
					"flex",
					"g++-multilib",
					"gcc-multilib",
					"git",
					"gnupg",
					"gperf",
					"imagemagick",
					"lib32ncurses-dev",
					"lib32readline-dev",
					"lib32z1-dev",
					"libdw-dev",
					"libelf-dev",
					"libgnutls28-dev",
					"liblz4-tool",
					"libncurses6",
					"libncurses-dev",
					"libsdl1.2-dev",
					"libssl-dev",
					"libxml2",
					"libxml2-utils",
					"lz4",
					"lzop",
					"pngcrush",
					"protobuf-compiler",
					"python3-protobuf",
					"python-is-python3",
					"rsync",
					"schedtool",
					"squashfs-tools",
					"xsltproc",
					"xxd",
					"zip",
					"zlib1g-dev",
					"android-sdk-platform-tools",
					"erofs-utils",
					"git-lfs",
				}

				// The user can also install their own deps
				// from the yaml file too. This just makes life
				// so much easier when building a striaght forward
				// build from lineage.
				// A mirror hiccup here would fail the whole build, so
				// apt and curl retry before giving up.
				apt := "apt-get -o Acquire::Retries=5 -o Dpkg::Options::=--force-confold"
				dep_install_command := fmt.Sprintf("%s install -y -qq %s",
					apt, strings.Join(deps, " "))

				commands := []string{
					"export DEBIAN_FRONTEND=noninteractive",
					apt + " update -qq",
					apt + " upgrade -y -qq",
					dep_install_command,
					"curl -fsSL --retry 5 --retry-all-errors -o /usr/bin/repo https://storage.googleapis.com/git-repo-downloads/repo",
					"chmod a+x /usr/bin/repo",
					"git config --global user.email \"ham@antonyjr.in\"",
					"git config --global user.name \"Hetzner Android Make\"",
					// No ccache: every build runs on a fresh server, so
					// the cache is always cold and only adds overhead.
					// A rerun on a kept server is incremental through out/.
					// zram disk sized to RAM (32 GB). zstd only consumes
					// RAM for pages that actually get swapped. On Ubuntu
					// 24.04 zram.ko ships in linux-modules-extra, which a
					// cloud image may lack. zram is an optimisation, the
					// recipe's swap file is the safety net, so any failure
					// here only prints a warning.
					apt + " install -y -qq linux-modules-extra-$(uname -r) zram-tools || echo 'zram: pakiety niedostępne'",
					"if modprobe zram; then " +
						"printf '%s\\n' ALGO=zstd PERCENT=100 PRIORITY=100 > /etc/default/zramswap && " +
						"systemctl enable zramswap.service && systemctl restart zramswap.service && " +
						"sysctl -w vm.swappiness=180 && sysctl -w vm.page-cluster=0 && swapon --show " +
						"|| echo 'zram: konfiguracja nieudana, build idzie bez zram'; " +
						"else echo 'zram: brak modułu, build idzie bez zram'; fi",
				}

				for varName, varValue := range vars {
					varName = strings.ToUpper(varName)
					varName = strings.ReplaceAll(varName, " ", "_")
					varName = strings.ReplaceAll(varName, "-", "_")
					varValue = strings.ReplaceAll(varValue, "\"", "\\\"")
					cmd := fmt.Sprintf("echo 'export %s=\"%s\"' >> ~/.bashrc", varName, varValue)
					commands = append(commands, cmd)
				}

				status.Status = "Installing Dependencies"
				status.Title = "Installing Dependencies"

				for indx, com := range commands {
					if status.Quit {
						hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
						time.Sleep(time.Minute * time.Duration(1))
						return errors.New("User Quit the Build")
					}

					err := term.ExecTerminal(indx, com)
					if err != nil {
						hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
						return checkErrorStatus(&status, errors.New("Prebuild Failed ("+err.Error()+")"))
					}

					err = term.WaitTerminal(indx)
					if err != nil {
						hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
						return checkErrorStatus(&status, errors.New("Prebuild Failed ("+err.Error()+")"))
					}

				}

				term.CloseTerminal()
			}

			// Start Executing Recipe Commands.
			terminal, err := NewTerminal(buildID)
			if err != nil {
				hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
				return checkErrorStatus(&status, err)
			}
			defer terminal.CloseTerminal()

			// Change directory to /ham-build
			err = terminal.ExecTerminal(-1, "mkdir -p /ham-build; cd /ham-build")
			if err != nil {
				hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
				return checkErrorStatus(&status, errors.New("Cannot Change to /ham-build Directory"))
			}
			err = terminal.WaitTerminal(-1)
			if err != nil {
				hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
				return checkErrorStatus(&status, errors.New("Cannot Change to /ham-build Directory"))
			}

			buildLen := len(hf.Build)
			for index, el := range hf.Build {
				if index < argv.FromStep {
					fmt.Printf("Skipping entry %d (%s): --from-step %d\n", index, el.Title, argv.FromStep)
					continue
				}
				failedStep = index
				if status.Quit {
					hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
					time.Sleep(time.Minute * time.Duration(1))
					return errors.New("User Quit the Build")
				}

				status.Status = "Building"
				status.Title = el.Title
				if status.Error != nil {
					hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
					return checkErrorStatus(&status, status.Error)
				}

				err := checkErrorStatus(&status, terminal.ExecTerminal(index, el.Cmd))
				if err != nil {
					hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
					return err
				}

				err = checkErrorStatus(&status, terminal.WaitTerminal(index))
				if err != nil {
					hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
					return err
				}

				// Avoid Premature Close When Tracking
				percent := int((float32(index) * 100.0) / float32(buildLen))
				if percent >= 1.0 {
					percent = percent - 1.0
				}
				status.Percentage = percent
			}

			failedStep = len(hf.Build)
			status.Percentage = 99
			status.Status = "Finished"
			status.Title = "Build Finished"
			fmt.Println("Built Successfully.")
			fmt.Println("Running Post Build Script... ")

			status.Status = "Post Build"
			status.Title = "Running Post Build"

			pbTerminal, err := NewTerminal(buildID + "-postbuild")
			if err != nil {
				hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
				return checkErrorStatus(&status, err)
			}
			defer pbTerminal.CloseTerminal()

			// Change directory to /ham-build
			err = pbTerminal.ExecTerminal(-1, "cd /ham-build")
			if err != nil {
				hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
				return checkErrorStatus(&status, errors.New("Cannot Change to /ham-build Directory"))
			}
			err = pbTerminal.WaitTerminal(-1)
			if err != nil {
				hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
				return checkErrorStatus(&status, errors.New("Cannot Change to /ham-build Directory"))
			}

			for index, cmd := range hf.PostBuild {
				if status.Quit {
					hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
					time.Sleep(time.Minute * time.Duration(1))
					return errors.New("User Quit the Build")
				}

				err := pbTerminal.ExecTerminal(index, cmd)
				if err != nil {
					hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
					return checkErrorStatus(&status, errors.New("Postbuild Failed ("+err.Error()+")"))
				}

				err = pbTerminal.WaitTerminal(index)
				if err != nil {
					hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "failed")
					return checkErrorStatus(&status, errors.New("Postbuild Failed ("+err.Error()+")"))
				}
			}

			hamSSHKey, _ = helpers.UpdateSSHKeyLabel(&client.SSHKey, hamSSHKey, serverName, "successful")
			succeeded = true
			status.Percentage = 100
			status.Status = "Finished"
			status.Title = "Completed"

			fmt.Println("Finished Build")

			// Give Some Time for Clients to Fetch this Status
			time.Sleep(time.Minute * time.Duration(1))
			return nil
		},
	}
}

func checkErrorStatus(state *statusT, err error) error {
	// Set Build to Error
	// We will wait for 2 mins before we exit setting
	// the status of the build at hetzner labels.
	if err == nil {
		return nil
	}

	state.Status = "Build Failed"
	state.Title = err.Error()
	state.Error = err
	state.Percentage = 100

	time.Sleep(time.Minute * time.Duration(2))
	return err
}

func destroyCurrentServer(client *hcloud.Client, UniqueID string) {
	serverName := helpers.ServerNameFromSHA256(UniqueID)
	fmt.Println("Destroying ", serverName)

	releaseBuildVolume()
	err := helpers.DestroySelf(client, serverName)
	if err == nil {
		return
	}

	// The server is the expensive part. A volume left behind is
	// removed by the next `ham get` or `ham clean`.
	fmt.Println("Self Destroy Failed: ", err.Error())
	err = helpers.DeleteServer(client, serverName)
	if err != nil {
		fmt.Println("Server Destroy Failed: ", err.Error())
	}
}

// Transient systemd unit on the build server that deletes a kept,
// failed build after FailedServerTTL. Kept servers used to run until
// the client or its watchdog came back, which costs money when that
// machine is switched off. `touch /tmp/ham-keep` or
// `systemctl stop ham-ttl.timer` keeps the server. 12 hours: a failure
// at night is still there in the morning, and the extra hours cost
// less than rebuilding out/ on a new server.
const (
	FailedServerUnit = "ham-ttl"
	FailedServerTTL  = 12 * time.Hour
	KeepServerFile   = "/tmp/ham-keep"
)

func scheduleFailedServerCleanup(UniqueID string) {
	self, err := os.Executable()
	if err != nil {
		self = "/usr/bin/ham"
	}

	_ = exec.Command("systemctl", "stop", FailedServerUnit+".timer", FailedServerUnit+".service").Run()
	out, err := exec.Command("systemd-run",
		"--unit="+FailedServerUnit,
		fmt.Sprintf("--on-active=%ds", int(FailedServerTTL.Seconds())),
		self, "destroy-self", "--sum", UniqueID).CombinedOutput()
	if err != nil {
		fmt.Println("Cannot Schedule Server Cleanup: ", err.Error(), string(out))
		return
	}
	fmt.Printf("Failed build kept. Server deletes itself in %s unless %s exists.\n",
		FailedServerTTL, KeepServerFile)
}

// The volume is detached while this server still runs, so nothing
// may use it any more: swap files on it go first (the recipe may put
// one there), then the mount.
func releaseBuildVolume() {
	// A server with a big enough disk builds without a volume;
	// /ham-build is then a plain directory and nothing is detached.
	if exec.Command("mountpoint", "-q", "/ham-build").Run() != nil {
		return
	}

	swaps, err := os.ReadFile("/proc/swaps")
	if err == nil {
		for _, line := range strings.Split(string(swaps), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && strings.HasPrefix(fields[0], "/ham-build/") {
				out, err := exec.Command("swapoff", fields[0]).CombinedOutput()
				if err != nil {
					fmt.Println("swapoff ", fields[0], ": ", err.Error(), string(out))
				}
			}
		}
	}

	_ = exec.Command("sync").Run()
	if exec.Command("umount", "/ham-build").Run() != nil {
		_ = exec.Command("umount", "-l", "/ham-build").Run()
	}
}

func statusServer(state *statusT) {
	// Local only: the client reads the status through SSH (ham
	// build-status), and an open port took an unauthenticated "quit".
	listener, err := net.Listen("tcp", "127.0.0.1:1695")
	if err != nil {
		state.Error = err
		return
	}

	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		go handleRequest(state, conn)
	}
}

func handleRequest(state *statusT, conn net.Conn) {
	buf := make([]byte, 1024)
	rLen, err := conn.Read(buf)
	if err != nil {
		return
	}

	request := strings.ToLower(string(buf[:rLen]))
	var resp string
	if state.Error != nil {
		resp = fmt.Sprintf("{ \"error\": true, \"message\": \"%s\" }\n",
			state.Error)
	} else if request == "status" {
		resp = fmt.Sprintf("{ \"error\": false, \"status\": \"%s\", \"progress\": \"%s\", \"percentage\": %d }\n",
			state.Status, state.Title, state.Percentage)
	} else if request == "quit" {
		resp = fmt.Sprintf("{ \"error\": false, \"status\": \"Stopping\", \"progress\": \"Stopping\", \"percentage\": %d }\n",
			state.Percentage)
		state.Status = "Stopping Build"
		state.Title = "Stopping Build"
		state.Quit = true
	} else {
		resp = fmt.Sprintf("{ \"error\": true, \"message\": \"Unknown command\" }\n")
	}

	conn.Write([]byte(resp))
	conn.Close()
}

func entryTitle(build []struct {
	Title string `yaml:"name"`
	Cmd   string `yaml:"run"`
}, step int) string {
	switch {
	case step < 0:
		return "prebuild"
	case step >= len(build):
		return "post_build"
	default:
		return build[step].Title
	}
}

// on_failure entries of the recipe, in their own terminal, after a failed
// build. Best effort: errors are printed, and the whole run is cut off after
// 10 minutes so that cleanup scheduling still happens.
func runOnFailure(cmds []string, uid string, step int, title string) {
	if len(cmds) == 0 {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		term, err := NewTerminal(uid + "-onfailure")
		if err != nil {
			fmt.Println("on_failure: ", err.Error())
			return
		}
		defer term.CloseTerminal()
		setup := fmt.Sprintf("cd /ham-build 2>/dev/null || cd /; export HAM_FAILED_STEP=%d HAM_FAILED_TITLE='%s'",
			step, strings.ReplaceAll(title, "'", "'\\''"))
		if err := term.ExecTerminal(-1, setup); err == nil {
			err = term.WaitTerminal(-1)
		}
		for index, cmd := range cmds {
			if err := term.ExecTerminal(index, cmd); err != nil {
				fmt.Println("on_failure: ", err.Error())
				return
			}
			if err := term.WaitTerminal(index); err != nil {
				fmt.Println("on_failure: ", err.Error())
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Minute):
		fmt.Println("on_failure: timeout after 10 minutes")
	}
}
