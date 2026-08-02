package obs

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// VerifyTestMain runs m.Run() itself and calls os.Exit with its result -
	// it never returns. A trailing os.Exit(m.Run()) here would be dead code
	// that reads like a (harmless but confusing) double-run.
	goleak.VerifyTestMain(m)
}
