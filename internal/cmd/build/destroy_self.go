package build

import (
	"fmt"
	"os"

	"github.com/antony-jr/ham/internal/core"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/mkideal/cli"
)

type destroySelfT struct {
	cli.Helper
	Sum string `cli:"*s,sum" usage:"SHA256 Hash of the main ham.yaml file"`
}

// Run by the ham-ttl timer after a failed build was kept (see
// scheduleFailedServerCleanup).
func NewDestroySelfCommand() *cli.Command {
	return &cli.Command{
		Name: "destroy-self",
		Desc: "Delete this build server and its volume unless " + KeepServerFile + " exists. (*Run in Build Machine) (Private)",
		Argv: func() interface{} { return new(destroySelfT) },
		Fn: func(ctx *cli.Context) error {
			argv := ctx.Argv().(*destroySelfT)

			if _, err := os.Stat(KeepServerFile); err == nil {
				fmt.Println(KeepServerFile, "exists, keeping the server.")
				return nil
			}

			config, err := core.GetConfiguration()
			if err != nil {
				return err
			}

			destroyCurrentServer(hcloud.NewClient(hcloud.WithToken(config.APIKey)), argv.Sum)
			return nil
		},
	}
}
