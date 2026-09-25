package helpers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"encoding/json"
	"io/ioutil"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// Short SHA256 Sum is used since sha256 sum is simply too
// long for a server name in Hetzner.
// It's concatenation of first 7 characters and last 7 characters
// of the original sha256 sum hex.
func ServerNameFromSHA256(sum string) string {
	shortSHA256Sum := sum[:7] + sum[len(sum)-7:]
	serverName := fmt.Sprintf("build-%s", shortSHA256Sum)
	return serverName
}

var (
	ErrServerNotFound = errors.New("Server Not Found")
	ErrVolumeNotFound = errors.New("Volume Not Found")
)

// Remove all servers at hetzner project which is running
// beyond 24 hours. This is because we want to reduce cost
// at all times and we really don't want dead expesive servers
// running around wasting our money.
// So to accomplish that we simply destroy servers older than
// 24 hours or with age of 24 hours.
// We destroy servers whenever we see them or connect with
// the api.
//
// "If you are 1 day old then you are dead to me."
// -- Antony J.R
func DestroyAllDeadServers(client *hcloud.Client) error {
	servers, err := client.Server.All(
		context.Background(),
	)

	if err != nil {
		return err
	}

	now := time.Now().UTC()

	for _, server := range servers {
		if !strings.HasPrefix(server.Name, "build-") {
			continue
		}

		if now.Sub(server.Created) >= 24*time.Hour {
			_ = TryDeleteServer(client, server.Name, 5, 5)
		}
	}

	return DestroyOrphanVolumes(client)
}

// A server deleted from the inside, or deleted without waiting,
// leaves its build-*-vol volume detached. Nothing lists volumes
// otherwise, so they would be billed forever. Fresh volumes are
// skipped: CreateServer attaches the volume a few seconds after
// creating it.
func DestroyOrphanVolumes(client *hcloud.Client) error {
	vols, err := client.Volume.All(
		context.Background(),
	)

	if err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, volume := range vols {
		if !strings.HasPrefix(volume.Name, "build-") ||
			!strings.HasSuffix(volume.Name, "-vol") ||
			volume.Server != nil ||
			now.Sub(volume.Created) < 30*time.Minute {
			continue
		}

		fmt.Println("Destroying Orphan Volume: ", volume.Name)
		_, err := client.Volume.Delete(
			context.Background(),
			volume,
		)
		if err != nil {
			fmt.Println("Volume Destroy Error: ", err.Error())
		}
	}

	return nil
}

// Delete the server, wait until Hetzner has finished (which also
// detaches the volume) and then delete the volume. A resource that
// is already gone counts as deleted.
func TryDeleteServer(client *hcloud.Client, serverName string, maxTries int, interval int) error {
	var lastErr error
	for try := 0; try <= maxTries; try++ {
		if try > 0 {
			fmt.Println("Destroying Server Failed. Retrying... ", lastErr.Error())
			time.Sleep(time.Second * time.Duration(interval))
		}

		err := DeleteServer(client, serverName)
		if err != nil && !errors.Is(err, ErrServerNotFound) {
			lastErr = err
			continue
		}

		err = DeleteVolume(&client.Volume, serverName)
		if err == nil || errors.Is(err, ErrVolumeNotFound) {
			return nil
		}
		lastErr = err
	}

	return errors.New("Cannot Destroy Remote Server or Volume. " + lastErr.Error())
}

func DeleteServer(client *hcloud.Client, serverName string) error {
	server, _, err := client.Server.GetByName(
		context.Background(),
		serverName,
	)

	if err != nil {
		return err
	}

	if server == nil {
		return ErrServerNotFound
	}

	result, _, err := client.Server.DeleteWithResult(
		context.Background(),
		server,
	)

	if err != nil {
		return err
	}

	return client.Action.WaitFor(context.Background(), result.Action)
}

func GetVolumeLinuxDeviceForServer(client *hcloud.Client, serverName string) (string, error) {
	volName := fmt.Sprintf("%s-vol", serverName)
	vols, err := client.Volume.All(
		context.Background(),
	)

	if err != nil {
		return "", err
	}

	for _, volume := range vols {
		if volume.Name == volName {
			return volume.LinuxDevice, nil
		}
	}

	return "", errors.New("No Such Volume")

}

func DeleteVolume(vclient *hcloud.VolumeClient, serverName string) error {
	volName := fmt.Sprintf("%s-vol", serverName)
	volume, _, err := vclient.GetByName(
		context.Background(),
		volName,
	)

	if err != nil {
		return err
	}

	if volume == nil {
		return ErrVolumeNotFound
	}

	_, err = vclient.Delete(
		context.Background(),
		volume,
	)

	return err
}

// Run on the build server itself after a successful build. The
// server cannot delete its volume once it is gone, so the order is
// detach volume, delete volume, delete server. The caller must have
// unmounted the volume and turned off any swap on it.
func DestroySelf(client *hcloud.Client, serverName string) error {
	volName := fmt.Sprintf("%s-vol", serverName)
	volume, _, err := client.Volume.GetByName(
		context.Background(),
		volName,
	)
	if err != nil {
		return err
	}

	if volume != nil {
		if volume.Server != nil {
			action, _, err := client.Volume.Detach(
				context.Background(),
				volume,
			)
			if err != nil {
				return err
			}

			err = client.Action.WaitFor(context.Background(), action)
			if err != nil {
				return err
			}
		}

		tries := 0
		for {
			_, err = client.Volume.Delete(
				context.Background(),
				volume,
			)
			if err == nil {
				break
			}

			tries++
			if tries > 20 {
				return err
			}
			time.Sleep(time.Second * time.Duration(5))
		}
	}

	// Deleting ourselves ends this process, so this goes last.
	err = DeleteServer(client, serverName)
	if errors.Is(err, ErrServerNotFound) {
		return nil
	}
	return err
}

func GetServerAgeInHours(sclient *hcloud.ServerClient, serverName string) (int, error) {
	servers, err := sclient.All(
		context.Background(),
	)

	if err != nil {
		return -1, err
	}

	loc, err := time.LoadLocation("UTC")
	if err != nil {
		return -1, err
	}

	for _, server := range servers {
		if server.Name == serverName {
			now := time.Now().In(loc)
			diff := now.Sub(server.Created)
			hours := int(diff.Hours())

			return hours, nil
		}
	}

	return -1, ErrServerNotFound
}

func UpdateSSHKeyLabel(sclient *hcloud.SSHKeyClient, sshKey *hcloud.SSHKey, key string, value string) (*hcloud.SSHKey, error) {
	lbls := sshKey.Labels
	lbls[key] = value

	newKey, _, err := sclient.Update(
		context.Background(),
		sshKey,
		hcloud.SSHKeyUpdateOpts{
			Name:   sshKey.Name,
			Labels: lbls,
		},
	)

	return newKey, err
}

func ReadVarsJsonFile(path string) (map[string]string, error) {
	ret := map[string]string{}
	source, err := ioutil.ReadFile(path)
	if err != nil {
		return ret, err
	}

	var result map[string]interface{}
	err = json.Unmarshal(source, &result)
	if err != nil {
		return ret, err
	}

	for key, value := range result {
		ret[key] = value.(interface{}).(string)
	}

	return ret, nil
}
