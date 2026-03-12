package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/sstallion/go-hid"
)

const (
	razerVID uint16 = 0x1532

	wiredProductID    uint16 = 0x007A
	wirelessProductID uint16 = 0x007B
	dockProductID     uint16 = 0x007E
	dockProProductID  uint16 = 0x00A4

	featureReportSize = 91

	statusSuccess      byte = 0x02
	statusBusy         byte = 0x01
	statusNotSupported byte = 0x05
	batteryRawMax      byte = 0xFF
	batteryClass       byte = 0x07
	batteryPayloadSize byte = 0x02
	batteryCommandID   byte = 0x80
	chargingCommandID  byte = 0x84

	defaultTransactionID byte = 0xFF
	altTransactionID1    byte = 0x1F
	altTransactionID2    byte = 0x3F

	queryBusyRetryLimit = 10
	queryBusyRetryDelay = 10 * time.Millisecond

	outputFormatCompact  = "compact"
	outputFormatHuman    = "human"
	outputFormatKeyValue = "keyvalue"

	iconMouse = "󰍽"

	iconBatteryFull     = "󰁹"
	iconBatteryHigh     = "󰂁"
	iconBatteryMid      = "󰁾"
	iconBatteryLow      = "󰁻"
	iconBatteryCritical = "󰁺"
	iconBatteryCharging = "󰂄"

	iconDocked = ""
)

type candidate struct {
	path         string
	productStr   string
	serial       string
	interfaceNbr int
}

type batteryResult struct {
	percent  int
	charging bool
	docked   bool
	path     string
}

func main() {
	productIDFlag := flag.Uint("pid", 0, "Razer product ID in hex/decimal (e.g. 0x007A). 0 probes wired+wireless defaults")
	verbose := flag.Bool("v", false, "verbose logging")
	format := flag.String("format", outputFormatCompact, "output format: compact|human|keyvalue")
	flag.Parse()

	if err := hid.Init(); err != nil {
		log.Fatalf("failed to init hid: %v", err)
	}
	defer func() {
		_ = hid.Exit()
	}()

	productIDs := []uint16{wiredProductID, wirelessProductID}
	if *productIDFlag != 0 {
		productIDs = []uint16{uint16(*productIDFlag)}
	}

	devices, err := enumerateCandidatesForProductIDs(productIDs)
	if err != nil {
		log.Fatalf("failed to enumerate devices: %v", err)
	}
	if len(devices) == 0 {
		if *productIDFlag != 0 {
			log.Fatalf("no matching Razer devices found for pid=0x%04X", uint16(*productIDFlag))
		}
		log.Fatalf("no matching Razer devices found for default PIDs: 0x%04X, 0x%04X", wiredProductID, wirelessProductID)
	}

	if *verbose {
		for _, d := range devices {
			log.Printf("candidate path=%q iface=%d product=%q serial=%q", d.path, d.interfaceNbr, d.productStr, d.serial)
		}
	}

	dockPresent := detectDockPresent()
	if *verbose {
		log.Printf("dock present: %t", dockPresent)
	}

	result, err := probeBattery(devices, dockPresent)
	if err != nil {
		log.Fatalf("battery probe failed: %v", err)
	}

	switch *format {
	case outputFormatCompact:
		fmt.Printf("%s%d%%\n", statusIcon(result.percent, result.charging, result.docked), result.percent)
	case outputFormatHuman:
		fmt.Printf("Battery: %d%%\n", result.percent)
		fmt.Printf("Charging: %t\n", result.charging)
		fmt.Printf("Docked: %t\n", result.docked)
		fmt.Printf("Source: %s\n", result.path)
	case outputFormatKeyValue:
		fmt.Printf("percent=%d\n", result.percent)
		fmt.Printf("charging=%t\n", result.charging)
		fmt.Printf("docked=%t\n", result.docked)
		fmt.Printf("status=%s\n", statusLabel(result.charging, result.docked))
		fmt.Printf("source=%s\n", strconv.Quote(result.path))
	default:
		log.Fatalf("unsupported output format %q (expected %q, %q, or %q)", *format, outputFormatCompact, outputFormatHuman, outputFormatKeyValue)
	}
}

func detectDockPresent() bool {
	return hasDevicePID(dockProductID) || hasDevicePID(dockProProductID)
}

func hasDevicePID(pid uint16) bool {
	found := false
	err := hid.Enumerate(razerVID, pid, func(info *hid.DeviceInfo) error {
		if info != nil {
			found = true
		}
		return nil
	})
	return err == nil && found
}

func enumerateCandidates(productID uint16) ([]candidate, error) {
	items := make([]candidate, 0, 8)
	seen := make(map[string]struct{})

	err := hid.Enumerate(razerVID, productID, func(info *hid.DeviceInfo) error {
		if info == nil {
			return nil
		}
		if _, ok := seen[info.Path]; ok {
			return nil
		}
		seen[info.Path] = struct{}{}

		items = append(items, candidate{
			path:         info.Path,
			productStr:   info.ProductStr,
			serial:       info.SerialNbr,
			interfaceNbr: info.InterfaceNbr,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].interfaceNbr < items[j].interfaceNbr
	})
	return items, nil
}

func enumerateCandidatesForProductIDs(productIDs []uint16) ([]candidate, error) {
	all := make([]candidate, 0, 8)
	seen := make(map[string]struct{})

	for _, productID := range productIDs {
		items, err := enumerateCandidates(productID)
		if err != nil {
			return nil, err
		}

		for _, item := range items {
			if _, ok := seen[item.path]; ok {
				continue
			}
			seen[item.path] = struct{}{}
			all = append(all, item)
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		return all[i].interfaceNbr < all[j].interfaceNbr
	})

	return all, nil
}

func probeBattery(devices []candidate, dockPresent bool) (*batteryResult, error) {
	var errs []error
	for _, d := range devices {
		res, err := readBatteryOnPath(d, dockPresent)
		if err == nil {
			return res, nil
		}
		errs = append(errs, fmt.Errorf("path %s (iface %d): %w", d.path, d.interfaceNbr, err))
	}
	return nil, errors.Join(errs...)
}

func readBatteryOnPath(d candidate, dockPresent bool) (*batteryResult, error) {
	dev, err := hid.OpenPath(d.path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = dev.Close()
	}()

	transactionIDs := []byte{defaultTransactionID, altTransactionID1, altTransactionID2}
	var errs []error

	for _, transactionID := range transactionIDs {
		batteryPercent, err := queryBatteryPercent(dev, transactionID)
		if err != nil {
			errs = append(errs, fmt.Errorf("tid 0x%02X battery: %w", transactionID, err))
			continue
		}

		charging, err := queryCharging(dev, transactionID)
		if err != nil {
			errs = append(errs, fmt.Errorf("tid 0x%02X charging: %w", transactionID, err))
			continue
		}

		docked := charging && dockPresent

		return &batteryResult{
			percent:  batteryPercent,
			charging: charging,
			docked:   docked,
			path:     d.path,
		}, nil
	}

	return nil, errors.Join(errs...)
}

func queryBatteryPercent(dev *hid.Device, transactionID byte) (int, error) {
	resp, err := runQuery(dev, batteryCommandID, transactionID)
	if err != nil {
		return 0, fmt.Errorf("battery query failed: %w", err)
	}

	raw := resp[10]
	percent := max(int(raw)*100/int(batteryRawMax), 0)
	return min(percent, 100), nil
}

func queryCharging(dev *hid.Device, transactionID byte) (bool, error) {
	resp, err := runQuery(dev, chargingCommandID, transactionID)
	if err != nil {
		return false, fmt.Errorf("charging query failed: %w", err)
	}
	return resp[10] > 0, nil
}

func runQuery(dev *hid.Device, commandID byte, transactionID byte) ([]byte, error) {
	report := buildRazerQuery(commandID, transactionID)
	if _, err := dev.SendFeatureReport(report); err != nil {
		return nil, fmt.Errorf("send feature report: %w", err)
	}

	for attempt := range queryBusyRetryLimit {
		if attempt > 0 {
			time.Sleep(queryBusyRetryDelay)
		}

		resp := make([]byte, featureReportSize)
		resp[0] = 0x00
		if _, err := dev.GetFeatureReport(resp); err != nil {
			return nil, fmt.Errorf("get feature report: %w", err)
		}
		if len(resp) < 11 {
			return nil, fmt.Errorf("short response (%d bytes)", len(resp))
		}
		if resp[1] == statusSuccess {
			return resp, nil
		}
		if resp[1] == statusBusy {
			continue
		}
		if resp[1] == statusNotSupported {
			return nil, fmt.Errorf("unexpected status 0x%02X (command not supported)", resp[1])
		}
		return nil, fmt.Errorf("unexpected status 0x%02X", resp[1])
	}

	return nil, fmt.Errorf("device stayed busy after %d attempts", queryBusyRetryLimit)
}

func buildRazerQuery(commandID byte, transactionID byte) []byte {
	report := make([]byte, featureReportSize)
	report[0] = 0x00
	report[1] = 0x00 // status
	report[2] = transactionID
	report[3] = 0x00 // remaining packets (big-endian)
	report[4] = 0x00
	report[5] = 0x00 // protocol type
	report[6] = batteryPayloadSize
	report[7] = batteryClass
	report[8] = commandID
	report[9] = 0x00
	report[10] = 0x00
	report[89] = crcFor(report)
	report[90] = 0x00
	return report
}

func crcFor(report []byte) byte {
	var crc byte
	for i := 3; i <= 88 && i < len(report); i++ {
		crc ^= report[i]
	}
	return crc
}

func batteryIcon(percent int) string {
	if percent > 80 {
		return iconBatteryFull
	}
	if percent > 50 {
		return iconBatteryHigh
	}
	if percent > 25 {
		return iconBatteryMid
	}
	if percent > 10 {
		return iconBatteryLow
	}
	return iconBatteryCritical
}

func statusIcon(percent int, charging bool, docked bool) string {
	if charging || docked {
		return iconDocked
	}
	return iconMouse
}

func statusLabel(charging bool, docked bool) string {
	if docked {
		return "docked"
	}
	if charging {
		return "charging"
	}
	return "mouse"
}

func init() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr)
}
