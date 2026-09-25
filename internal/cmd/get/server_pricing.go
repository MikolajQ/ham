package get

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// Tried in order when no type is given on the command line.
//
// CPX62: 16 shared vCPU, 32 GB RAM, 640 GB local NVMe. Cheaper than
// CCX33 per hour and big enough to build on its own disk, so no
// volume is created.
//
// CCX33: 8 dedicated vCPU, 32 GB RAM, 240 GB disk plus a 400 GB
// volume. Used only when Hetzner has no CPX62 capacity anywhere.
var DefaultServerTypes = []string{"cpx62", "ccx33"}

type ServerCandidate struct {
	Type  *hcloud.ServerType
	Price float64
}

// Price lookup happens in this location. nbg1, fsn1 and hel1 have
// had the same rates so far.
const PriceLocation = "nbg1"

func ServerCandidates(client *hcloud.Client, names []string) ([]ServerCandidate, error) {
	pricing, _, err := client.Pricing.Get(context.Background())
	if err != nil {
		return nil, err
	}

	var candidates []ServerCandidate
	for _, name := range names {
		for _, server := range pricing.ServerTypes {
			if server.ServerType.Name != name {
				continue
			}

			for _, entry := range server.Pricings {
				if strings.ToLower(entry.Location.Name) != PriceLocation {
					continue
				}

				amount, err := strconv.ParseFloat(entry.Hourly.Gross, 64)
				if err != nil {
					return nil, errors.New("Invalid Price Given")
				}

				// Pricing only carries the type's name; the full type
				// (disk size, architecture) comes from the type API.
				serverType, _, err := client.ServerType.GetByName(context.Background(), name)
				if err != nil {
					return nil, err
				}
				if serverType == nil {
					break
				}

				candidates = append(candidates, ServerCandidate{serverType, amount})
				break
			}
		}
	}

	if len(candidates) == 0 {
		return nil, errors.New("Cannot Find Suitable Server (" + strings.Join(names, ", ") + ")")
	}
	return candidates, nil
}
