// Command runtime_diagnostics prints RobotGo's versioned, sanitized runtime
// support report without requesting desktop consent.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/marang/robotgo"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	report := robotgo.GetRuntimeDiagnostics(ctx)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(data))
}
