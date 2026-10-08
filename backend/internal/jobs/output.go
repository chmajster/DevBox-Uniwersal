package jobs

import "context"

type outputKey struct{}
type Output func(stream, line string)

func WithOutput(ctx context.Context, output Output) context.Context {
	return context.WithValue(ctx, outputKey{}, output)
}
func EmitOutput(ctx context.Context, stream, line string) {
	if output, ok := ctx.Value(outputKey{}).(Output); ok {
		output(stream, line)
	}
}
