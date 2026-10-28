// Clipboard access is opt-in: running without -read or -write only shows help.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/marang/robotgo/clipboard"
)

func main() {
	read := flag.Bool("read", false, "read the selected clipboard to stdout")
	write := flag.Bool("write", false, "replace the selected clipboard with text from stdin")
	primary := flag.Bool("primary", false, "use the primary selection instead of the regular clipboard")
	timeout := flag.Duration("timeout", 5*time.Second, "clipboard operation deadline")
	flag.Parse()
	if !*read && !*write {
		flag.Usage()
		return
	}
	if *read == *write || *timeout <= 0 || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "choose exactly one of -read or -write and a positive timeout; write text through stdin, not arguments")
		os.Exit(2)
	}
	selection := clipboard.SelectionClipboard
	if *primary {
		selection = clipboard.SelectionPrimary
	}
	var err error
	if *write {
		// Reading stdin is separate from the bounded clipboard operation. Pipe
		// finite input; no payload is included in the helper's command line.
		text, readErr := io.ReadAll(os.Stdin)
		if readErr != nil {
			err = readErr
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), *timeout)
			err = clipboard.WriteAllContext(ctx, string(text), selection)
			cancel()
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		text, readErr := clipboard.ReadAllContext(ctx, selection)
		cancel()
		if readErr != nil {
			err = readErr
		} else {
			_, err = io.WriteString(os.Stdout, text)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
