hl.monitor({
  output = "",
  mode = "1280x720@60",
  position = "0x0",
  scale = 1,
})

hl.config({
  general = {
    gaps_in = 0,
    gaps_out = 0,
    border_size = 0,
  },
  decoration = {
    rounding = 0,
    shadow = {
      enabled = false,
    },
    blur = {
      enabled = false,
    },
  },
  animations = {
    enabled = false,
  },
  input = {
    follow_mouse = 0,
  },
  misc = {
    disable_hyprland_logo = true,
    disable_splash_rendering = true,
  },
})

hl.window_rule({
  name = "robotgo-evidence-fixture",
  match = {
    class = "wev",
  },
  float = true,
  move = {120, 80},
  size = {480, 320},
  border_size = 0,
  no_anim = true,
})

