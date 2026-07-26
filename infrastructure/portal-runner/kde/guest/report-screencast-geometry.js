(function () {
    const service = "io.github.marang.robotgo.KDEPortalGeometry";
    const path = "/io/github/marang/robotgo/KDEPortalGeometry";
    const iface = "io.github.marang.robotgo.KDEPortalGeometry";
    const screen = workspace.virtualScreenSize;
    const dialog = workspace.activeClient;

    if (!dialog) {
        callDBus(
            service,
            path,
            iface,
            "Report",
            [
                Math.round(screen.width),
                Math.round(screen.height),
                -1,
                -1,
                -1,
                -1
            ].join(" ")
        );
        return;
    }
    callDBus(
        service,
        path,
        iface,
        "Report",
        [
            Math.round(screen.width),
            Math.round(screen.height),
            Math.round(dialog.x),
            Math.round(dialog.y),
            Math.round(dialog.width),
            Math.round(dialog.height)
        ].join(" ")
    );
})();
