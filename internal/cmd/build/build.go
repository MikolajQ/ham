package build

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
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

			if hf.SHA256Sum != argv.Sum {
				return errors.New("SHA256 Mismatch, Bad File.")
			}

			// We assume the current server name at hetzner to
			// be this and we use this assumption to destroy
			// the server when the build is done.
			serverName := helpers.ServerNameFromSHA256(hf.SHA256Sum)
			fmt.Printf("Build Server: %s\n", serverName)

			keepArg := ""
			if argv.KeepServer {
				keepArg = "--keep-server"
			}
			dctx := &daemon.Context{
				PidFileName: "/tmp/com.github.antony-jr.ham.pid",
				PidFilePerm: 0644,
				LogFileName: "/tmp/com.github.antony-jr.ham.log",
				LogFilePerm: 0640,
				WorkDir:     "./",
				Umask:       027,
				Args: []string{"ham",
					"build",
					keepArg,
					"-r",
					argv.RecipePath,
					"-a",
					argv.VarsPath,
					"-s",
					argv.Sum},
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
			defer func() {
				if succeeded || !argv.KeepServer {
					destroyCurrentServer(client, hf.SHA256Sum)
				}
			}()

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
				term, err := NewTerminal(hf.SHA256Sum + "-prebuild")
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
					"zram-tools",
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
					// zram disk sized to RAM (32 GB on CCX33). zstd only
					// consumes RAM for pages that actually get swapped.
					"printf '%s\\n' ALGO=zstd PERCENT=100 PRIORITY=100 > /etc/default/zramswap",
					"systemctl enable --now zramswap.service",
					"systemctl restart zramswap.service",
					"sysctl -w vm.swappiness=180",
					"sysctl -w vm.page-cluster=0",
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
			terminal, err := NewTerminal(hf.SHA256Sum)
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

			status.Percentage = 99
			status.Status = "Finished"
			status.Title = "Build Finished"
			fmt.Println("Built Successfully.")
			fmt.Println("Running Post Build Script... ")

			status.Status = "Post Build"
			status.Title = "Running Post Build"

			pbTerminal, err := NewTerminal(hf.SHA256Sum + "-postbuild")
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
	listener, err := net.Listen("tcp", "0.0.0.0:1695")
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
