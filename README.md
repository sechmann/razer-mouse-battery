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

`--format keyvalue` returns one `key=value` pair per line for simple regex parsing.

Example:

```text
percent=43
charging=false
docked=false
status=mouse
source="DevSrvsID:42950000683"
```

The compact format is useful for status bars like Sketchybar.

## Build and run

```bash
mise run build
./bin/razer-mouse-battery
```

## Flags

- `--pid <id>`: probe one product ID (hex or decimal)
- `--format compact|human|keyvalue`: output format (default: `compact`)
- `-v`: verbose debug output
