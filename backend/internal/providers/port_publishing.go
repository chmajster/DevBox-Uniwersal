package providers

import "context"

// PublishedPort maps a TCP port on all host interfaces to a container listener.
// It does not configure the application listener or terminate TLS.
type PublishedPort struct {
	HostPort      int `json:"host_port"`
	ContainerPort int `json:"container_port"`
}

// PortReservation distinguishes a new lease from an existing owned lease.
// Cleanup must never release a reused reservation for a running application.
type PortReservation struct {
	PortLease
	Reused bool
}

// SequentialPortAllocator extends exact reservations without changing the
// semantics of PortAllocator.Reserve or the networking module's manual API.
type SequentialPortAllocator interface {
	ReserveFrom(ctx context.Context, projectID, purpose string, start int) (PortReservation, error)
}

// PortLeaseOwner protects reuse and cleanup from releasing another project's lease.
type PortLeaseOwner interface {
	Owns(ctx context.Context, projectID, purpose string, port int) (bool, error)
	ReleaseOwned(ctx context.Context, projectID, purpose string, port int) error
}

type ComposePortTarget struct {
	Service       string
	ContainerPort int
}

// ComposePortPublisher configures only the selected web service. The returned
// rollback restores its previous override, never the user's Compose source.
type ComposePortPublisher interface {
	InspectComposePortTarget(ctx context.Context, directory, projectName, service string) (ComposePortTarget, error)
	ConfigureComposePorts(ctx context.Context, directory, projectName, service string, ports []PublishedPort) (func() error, error)
	CheckPublishedHTTP(ctx context.Context, port int) error
	ClearComposePorts(directory, projectName string) error
}
