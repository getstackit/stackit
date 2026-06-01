package merge

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
)

func requireYesInNonInteractive(ctx *app.Context, command string, yes, dryRun bool) error {
	if dryRun || yes || ctx.Interactive {
		return nil
	}
	return fmt.Errorf("%s requires --yes in non-interactive mode", command)
}
