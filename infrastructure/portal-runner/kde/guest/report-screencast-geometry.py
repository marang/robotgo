#!/usr/bin/python3
"""Receive one content-free KDE portal geometry report over private D-Bus."""

import sys

import dbus
import dbus.service
from dbus.mainloop.glib import DBusGMainLoop
from gi.repository import GLib


BUS_NAME = "io.github.marang.robotgo.KDEPortalGeometry"
OBJECT_PATH = "/io/github/marang/robotgo/KDEPortalGeometry"
INTERFACE = "io.github.marang.robotgo.KDEPortalGeometry"


class GeometryReceiver(dbus.service.Object):
    def __init__(self, bus, loop):
        self.loop = loop
        super().__init__(bus, OBJECT_PATH)

    @dbus.service.method(INTERFACE, in_signature="s", out_signature="")
    def Report(self, payload):
        fields = str(payload).split()
        if len(fields) != 6:
            print("error bridge-unavailable", flush=True)
            self.loop.quit()
            return
        try:
            values = [int(field) for field in fields]
        except ValueError:
            print("error bridge-unavailable", flush=True)
            self.loop.quit()
            return
        if values[2:] == [-1, -1, -1, -1]:
            print("error window-unavailable", flush=True)
        else:
            print("ok", *values, flush=True)
        self.loop.quit()


def main():
    DBusGMainLoop(set_as_default=True)
    bus = dbus.SessionBus()
    name = dbus.service.BusName(
        BUS_NAME,
        bus,
        allow_replacement=False,
        replace_existing=False,
        do_not_queue=True,
    )
    loop = GLib.MainLoop()
    receiver = GeometryReceiver(bus, loop)
    loop.run()
    receiver.remove_from_connection()
    del name


if __name__ == "__main__":
    try:
        main()
    except Exception:
        print("error bridge-unavailable")
        sys.exit(1)
