//go:build !linux

package executor

import "time"


func threadCPUTime() time.Duration {
    return 0
}
