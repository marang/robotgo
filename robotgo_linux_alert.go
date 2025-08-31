//go:build linux

package robotgo

// Alert show a alert window
// Displays alert with the attributes.
// If cancel button is not given, only the default button is displayed
//
// Examples:
//
//     robotgo.Alert("hi", "window", "ok", "cancel")
func Alert(title, msg string, args ...string) bool {
        defaultBtn, cancelBtn := alertArgs(args...)
        c := `xmessage -center ` + msg +
                ` -title ` + title + ` -buttons ` + defaultBtn + ":0,"
        if cancelBtn != "" {
                c += cancelBtn + ":1"
        }
        c += ` -default ` + defaultBtn
        c += ` -geometry 400x200`

        out, err := Run(c)
        if err != nil {
                return false
        }
        if string(out) == "1" {
                return false
        }
        return true
}
