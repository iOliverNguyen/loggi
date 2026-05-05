package docker

import (
	"context"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	dclient "github.com/docker/docker/client"
)

// ContainerInfo is the minimal container metadata the web UI needs to render
// a picker.
type ContainerInfo struct {
	ID      string   `json:"id"`
	Names   []string `json:"names"`
	Image   string   `json:"image"`
	State   string   `json:"state"`
	Status  string   `json:"status"`
	Created int64    `json:"created"`
}

// ListContainers returns running containers (All=false). Caller-side caching
// is fine since this opens and closes its own short-lived client.
func ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	cli, err := dclient.NewClientWithOpts(dclient.FromEnv, dclient.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cs, err := cli.ContainerList(cctx, container.ListOptions{All: false})
	if err != nil {
		return nil, err
	}
	out := make([]ContainerInfo, 0, len(cs))
	for _, c := range cs {
		names := make([]string, 0, len(c.Names))
		for _, n := range c.Names {
			names = append(names, strings.TrimPrefix(n, "/"))
		}
		out = append(out, ContainerInfo{
			ID:      c.ID,
			Names:   names,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Created: c.Created,
		})
	}
	return out, nil
}
