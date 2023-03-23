// Copyright Contributors to the Open Cluster Management project

package utils

import (
	"fmt"
	"os"
	"time"
)

func PrintMsg(msg string) {
	now := time.Now()
	fmt.Fprintf(os.Stdout, "[%s] %s\n", now.Format(time.RFC3339), msg)
}
