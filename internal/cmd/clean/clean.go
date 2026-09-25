package clean

import (
	"context"
	"fmt"
	"strings"

	"github.com/antony-jr/ham/internal/core"
	"github.com/antony-jr/ham/internal/helpers"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/mkideal/cli"
)

type cleanT struct {
	cli.Helper
}

func NewCommand() *cli.Command {
	return &cli.Command{
		Name: "clean",
		Desc: "Clean Servers in your Hetzner Project",
		Argv: func() interface{} { return new(cleanT) },
		Fn: func(ctx *cli.Context) error {
			// argv := ctx.Argv().(*cleanT)

			config, err := core.GetConfiguration()
			if err != nil {
				return err
			}

			client := hcloud.NewClient(hcloud.WithToken(config.APIKey))

			fmt.Println("Cleaning SSH Keys.")
			targetSSHKey, _, err := client.SSHKey.Get(
				context.Background(),
				"ham-ssh-key",
			)
			if err != nil {
				return err
			}

			_, _, err = client.SSHKey.Update(
				context.Background(),
				targetSSHKey,
				hcloud.SSHKeyUpdateOpts{
					Name:   targetSSHKey.Name,
					Labels: map[string]string{},
				},
			)
			if err != nil {
				return err
			}

			fmt.Println("Destroying all HAM Dead Servers.")
			err = helpers.DestroyAllDeadServers(client)
			if err != nil {
				return err
			}

			fmt.Println("Destroying all Ham Servers.")
			err = destroyHamServers(client)
			if err != nil {
				return err
			}

			fmt.Printf("\nCleaned up Hetzner Project.\n")

			return nil
		},
	}
}

func destroyHamServers(client *hcloud.Client) error {
	servers, err := client.Server.All(
		context.Background(),
	)

	if err != nil {
		return err
	}

	for _, server := range servers {
		if !strings.HasPrefix(server.Name, "build-") {
			continue
		}

		fmt.Printf("Destroying... %s\n", server.Name)
		err = helpers.TryDeleteServer(client, server.Name, 20, 5)
		if err != nil {
			return err
		}
	}

	return helpers.DestroyOrphanVolumes(client)
}
