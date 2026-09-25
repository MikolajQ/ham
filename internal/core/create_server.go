package core

import (
	"context"
	"errors"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

const (
	TargetImage = "ubuntu-24.04"
)

// Tried in order. nbg1 is the cheapest and most stable location;
// fsn1 and hel1 are only used when Hetzner has no capacity for the
// server type there. The volume is created in the same location.
var TargetLocations = []string{"nbg1", "fsn1", "hel1"}

func CreateServer(client *hcloud.Client, server *hcloud.ServerType, serverName string) (*hcloud.Server, error) {
	// Get Server Image
	serverImage, _, err := client.Image.GetForArchitecture(
		context.Background(),
		TargetImage,
		server.Architecture,
	)
	if err != nil {
		return nil, err
	}
	if serverImage == nil {
		return nil, errors.New("Image " + TargetImage + " not found")
	}

	// Get ham-ssh-key SSH Key
	sshKey, _, err := client.SSHKey.Get(
		context.Background(),
		"ham-ssh-key",
	)
	if err != nil {
		return nil, err
	}

	defKey, _, err := client.SSHKey.Get(
		context.Background(),
		"default",
	)

	sshList := []*hcloud.SSHKey{sshKey}
	if err == nil && defKey != nil {
		sshList = append(sshList, defKey)
	}

	var lastErr error
	for _, locationName := range TargetLocations {
		created, err := createServerAt(client, server, serverName, serverImage, sshList, locationName)
		if err == nil {
			return created, nil
		}

		lastErr = err
		if !isCapacityError(err) {
			return nil, err
		}
	}

	return nil, lastErr
}

func createServerAt(client *hcloud.Client, server *hcloud.ServerType, serverName string,
	serverImage *hcloud.Image, sshList []*hcloud.SSHKey, locationName string) (*hcloud.Server, error) {
	location, _, err := client.Location.Get(
		context.Background(),
		locationName,
	)
	if err != nil {
		return nil, err
	}
	if location == nil {
		return nil, errors.New("Location " + locationName + " not found")
	}

	startAfterCreate := true
	automountVol := false

	// We need Special Volume of Size 400 GB
	// to hold only the lineage os build,
	// this will future proof this app.
	volCreateOpts := hcloud.VolumeCreateOpts{
		Name:      serverName + "-vol",
		Size:      400,
		Location:  location,
		Automount: &automountVol,
	}

	err = volCreateOpts.Validate()
	if err != nil {
		return nil, err
	}

	// Create Volume of 400 GiB
	volCreateResult, _, err := client.Volume.Create(
		context.Background(),
		volCreateOpts,
	)
	if err != nil {
		return nil, err
	}

	deleteVolume := func() {
		_, _ = client.Volume.Delete(
			context.Background(),
			volCreateResult.Volume,
		)
	}

	if volCreateResult.Action != nil {
		err = client.Action.WaitFor(context.Background(), volCreateResult.Action)
		if err != nil {
			deleteVolume()
			return nil, err
		}
	}

	// Server Creation Options
	serverCreateOpts := hcloud.ServerCreateOpts{
		Name:             serverName,
		ServerType:       server,
		Image:            serverImage,
		SSHKeys:          sshList,
		Location:         location,
		StartAfterCreate: &startAfterCreate,
		Labels:           map[string]string{},
		PublicNet: &hcloud.ServerCreatePublicNet{
			EnableIPv4: true,
			EnableIPv6: false,
		},
		Volumes: []*hcloud.Volume{volCreateResult.Volume},
	}

	err = serverCreateOpts.Validate()
	if err != nil {
		deleteVolume()
		return nil, err
	}

	// Create Server at Hetzner
	createResult, _, err := client.Server.Create(
		context.Background(),
		serverCreateOpts,
	)
	if err != nil {
		deleteVolume()
		return nil, err
	}

	// Wait till we Success or Failure
	// result from Action that is currently
	// running.
	actions := append([]*hcloud.Action{createResult.Action}, createResult.NextActions...)
	err = client.Action.WaitFor(context.Background(), actions...)
	if err != nil {
		// The server may exist even though an action failed.
		delResult, _, delErr := client.Server.DeleteWithResult(context.Background(), createResult.Server)
		if delErr == nil {
			_ = client.Action.WaitFor(context.Background(), delResult.Action)
		}
		deleteVolume()
		return nil, err
	}

	return createResult.Server, nil
}

func isCapacityError(err error) bool {
	codes := []hcloud.ErrorCode{
		hcloud.ErrorCodeResourceUnavailable,
		hcloud.ErrorCodePlacementError,
	}
	if hcloud.IsError(err, codes...) {
		return true
	}

	var actionErr hcloud.ActionError
	if errors.As(err, &actionErr) {
		for _, code := range codes {
			if actionErr.Code == string(code) {
				return true
			}
		}
	}
	return false
}
