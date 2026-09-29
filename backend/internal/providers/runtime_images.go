package providers

import "context"

type RuntimeImage struct {
	Reference string `json:"reference"`
	ID        string `json:"id"`
	Size      string `json:"size,omitempty"`
	Digest    string `json:"digest,omitempty"`
}
type RuntimeImageProvider interface {
	RuntimeImages(context.Context) ([]RuntimeImage, error)
	PullImage(context.Context, string) error
	RemoveImage(context.Context, string) error
	InspectImage(context.Context, string) (map[string]any, error)
}
