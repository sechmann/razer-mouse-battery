# razer-mouse-battery

Read battery and charging state from supported Razer mice via HID.

## Output format

Default (`--format compact`):

```text
<mouse_or_dock_icon><percent>%
```

Examples:

```text
󰍽43%
43%
```

`--format human` returns readable field output instead of compact icon text.

The compact format is useful for status bars like Sketchybar.

## Build and run

```bash
mise run build
./bin/razer-mouse-battery
```

## Flags

- `--pid <id>`: probe one product ID (hex or decimal)
- `--format compact|human`: output format (default: `compact`)
- `-v`: verbose debug output
