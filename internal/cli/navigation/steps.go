package navigation

import (
	"errors"
	"fmt"
	"strconv"
)

func parsePositiveSteps(args []string, steps int) (int, error) {
	if len(args) > 0 {
		parsedSteps, err := strconv.Atoi(args[0])
		if err != nil {
			return 0, fmt.Errorf("invalid steps argument: %s (must be a number)", args[0])
		}
		steps = parsedSteps
	}

	if steps < 1 {
		return 0, errors.New("steps must be at least 1")
	}

	return steps, nil
}
