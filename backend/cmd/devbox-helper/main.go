//go:build linux

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	devsystem "github.com/chmajster/DevBox-Uniwersal/backend/internal/system"
)

func main() {
	if os.Geteuid() != 0 {
		fatal("devbox-helper must run as root")
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	helper := devsystem.NewPrivilegedHelper()
	var err error
	switch os.Args[1] {
	case "install-package":
		if len(os.Args) != 3 {
			usage()
			os.Exit(2)
		}
		err = helper.InstallPackage(ctx, os.Args[2])
	case "restart-service":
		if len(os.Args) != 3 {
			usage()
			os.Exit(2)
		}
		err = helper.RestartService(ctx, os.Args[2])
	case "reload-nginx":
		if len(os.Args) != 2 {
			usage()
			os.Exit(2)
		}
		err = helper.ReloadNginx(ctx)
	case "write-config":
		if len(os.Args) != 3 {
			usage()
			os.Exit(2)
		}
		body, readErr := io.ReadAll(io.LimitReader(os.Stdin, 64*1024+1))
		if readErr != nil {
			err = readErr
		} else {
			err = helper.WriteControlledConfig(os.Args[2], body)
		}
	default:
		err = fmt.Errorf("%w: %s", devsystem.ErrOperationNotAllowed, os.Args[1])
	}
	if err != nil {
		fatal(err.Error())
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: devbox-helper install-package <component> | restart-service <service> | reload-nginx | write-config devbox-env")
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "[FAIL]", message)
	os.Exit(1)
}
