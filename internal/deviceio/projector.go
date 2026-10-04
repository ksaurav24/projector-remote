package deviceio

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	keystoneURI      = "content://jx.keystone.provider/keystone"
	focusManualURI   = "content://jx.focusmanual.provider/manualfocus"
	focusSettingsURI = "content://jx.focussettings.provider/focussettings"
	projectorTypeURI = "content://jx.projectortype.provider/projectorType"

	minKeystoneAngle    = -40
	maxKeystoneAngle    = 40
	focusWatchdog       = 1200 * time.Millisecond
	contentCallTimeout  = 8 * time.Second
	settingsReadWorkers = 2
	cornerMaxX          = 64
	cornerMaxY          = 36
	projectorWidth      = 1920.0
	projectorHeight     = 1080.0
	keystoneProperty    = "persist.vendor.hwc.keystone"
)

// ProjectorSettings is the user-visible projection state exposed by the
// vendor's exported content providers. Zoom is the actual projected area as a
// percentage of the uncorrected panel, not the settings app's historical
// jstep counter.
type ProjectorSettings struct {
	Zoom       int                    `json:"zoom"`
	MinZoom    int                    `json:"minZoom"`
	Horizontal int                    `json:"horizontal"`
	Vertical   int                    `json:"vertical"`
	Mode       string                 `json:"mode"`
	Features   map[string]bool        `json:"features"`
	Corners    map[string]CornerPoint `json:"corners"`
	CornerMaxX int                    `json:"cornerMaxX"`
	CornerMaxY int                    `json:"cornerMaxY"`
}

type CornerPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type contentCaller interface {
	Call(uri, method string, extras ...string) (string, error)
}

type execContentCaller struct{}

type propertyCaller interface {
	Get(name string) (string, error)
	Set(name, value string) error
	CornerSteps() (map[string]CornerPoint, error)
	SetCornerSteps(corner string, point CornerPoint) error
}

type execPropertyCaller struct{}

func (execPropertyCaller) Get(name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "getprop", name).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("deviceio: getprop %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (execPropertyCaller) Set(name, value string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "setprop", name, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("deviceio: setprop %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	// HWC reads this property while composing a frame. On an otherwise static
	// screen Android may not schedule another frame for several seconds, so the
	// new geometry is stored immediately but appears late. This supported debug
	// transaction invalidates the current frame; the server runs as root, which
	// has the required SurfaceFlinger permission.
	_, _ = exec.CommandContext(ctx, "service", "call", "SurfaceFlinger", "1004").CombinedOutput()
	return nil
}

var cornerSettingKeys = map[string][2]string{
	"lt": {"jstep_four_point_trapezoid_ltx", "jstep_four_point_trapezoid_lty"},
	"rt": {"jstep_four_point_trapezoid_rtx", "jstep_four_point_trapezoid_rty"},
	"rb": {"jstep_four_point_trapezoid_rbx", "jstep_four_point_trapezoid_rby"},
	"lb": {"jstep_four_point_trapezoid_lbx", "jstep_four_point_trapezoid_lby"},
}

func (execPropertyCaller) CornerSteps() (map[string]CornerPoint, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "settings", "list", "global").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("deviceio: read corner settings: %w: %s", err, strings.TrimSpace(string(out)))
	}
	values := make(map[string]int, 8)
	for _, line := range strings.Split(string(out), "\n") {
		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value, parseErr := strconv.Atoi(strings.TrimSpace(raw))
		if parseErr == nil {
			values[key] = value
		}
	}
	points := make(map[string]CornerPoint, 4)
	for corner, keys := range cornerSettingKeys {
		points[corner] = CornerPoint{
			X: min(max(values[keys[0]], 0), cornerMaxX),
			Y: min(max(values[keys[1]], 0), cornerMaxY),
		}
	}
	return points, nil
}

func (execPropertyCaller) SetCornerSteps(corner string, point CornerPoint) error {
	keys := cornerSettingKeys[corner]
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, item := range []struct {
		key   string
		value int
	}{{keys[0], point.X}, {keys[1], point.Y}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, "settings", "put", "global", item.key, strconv.Itoa(item.value)).CombinedOutput()
			if err != nil {
				errs <- fmt.Errorf("deviceio: write %s: %w: %s", item.key, err, strings.TrimSpace(string(out)))
			}
		}()
	}
	wg.Wait()
	close(errs)
	return <-errs
}

func (execContentCaller) Call(uri, method string, extras ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), contentCallTimeout)
	defer cancel()
	args := []string{"call", "--uri", uri, "--method", method}
	for _, extra := range extras {
		args = append(args, "--extra", extra)
	}
	out, err := exec.CommandContext(ctx, "content", args...).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("deviceio: content %s timed out after %s: %w", method, contentCallTimeout, ctx.Err())
		}
		return "", fmt.Errorf("deviceio: content %s: %w: %s", method, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

var bundleValueRE = regexp.MustCompile(`(?:^|[,{])([^=,{]+)=([^,}\]]+)`)

func bundleValue(output, key string) (string, error) {
	for _, match := range bundleValueRE.FindAllStringSubmatch(output, -1) {
		if strings.TrimSpace(match[1]) == key {
			return strings.TrimSpace(match[2]), nil
		}
	}
	return "", fmt.Errorf("deviceio: %q missing from content result %q", key, strings.TrimSpace(output))
}

func bundleInt(output, key string) (int, error) {
	v, err := bundleValue(output, key)
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("deviceio: parse %s=%q: %w", key, v, err)
	}
	return int(math.Round(f)), nil
}

func bundleBool(output, key string) (bool, error) {
	v, err := bundleValue(output, key)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(v)
}

var featureKeys = []string{
	"autoFocus",
	"trapezoidAuto",
	"autoBalance",
	"autoAvoidance",
	"autoCurtain",
	"trapezoidTB",
}

var projectionModes = []struct {
	name, query, set string
}{
	{"front", "isOrthographicForward", "orthographicForward"},
	{"rear", "isOrthographicRear", "orthographicRear"},
	{"ceiling-front", "isHoistingForward", "hoistingForward"},
	{"ceiling-rear", "isHoistingRear", "hoistingRear"},
}

// ProjectorSettings reads authoritative state from the vendor services.
func (c *Controller) ProjectorSettings() (ProjectorSettings, error) {
	// Prevent duplicate inventory scans from filling the provider queue. A
	// mutation does not take this lock, so it can run between individual reads.
	c.projectorReadMu.Lock()
	defer c.projectorReadMu.Unlock()

	state := ProjectorSettings{
		Features:   make(map[string]bool, len(featureKeys)),
		Corners:    make(map[string]CornerPoint, 4),
		CornerMaxX: cornerMaxX,
		CornerMaxY: cornerMaxY,
	}
	featureEnabled := make([]bool, len(featureKeys))
	jobs := []func() error{
		func() (err error) {
			state.Zoom, err = c.readContentInt(keystoneURI, "readCurrentScreenPercentage", "key")
			return
		},
		func() (err error) {
			state.MinZoom, err = c.readContentInt(keystoneURI, "readMinScreenPercentage", "key")
			return
		},
		func() (err error) {
			state.Horizontal, err = c.readContentInt(keystoneURI, "angleByHorizontal", "angleByHorizontal")
			return
		},
		func() (err error) {
			state.Vertical, err = c.readContentInt(keystoneURI, "angleByVertical", "angleByVertical")
			return
		},
		func() error {
			points, err := c.propertyRun.CornerSteps()
			if err != nil {
				return err
			}
			state.Corners = points
			c.projectorMu.Lock()
			c.cornerSteps = cloneCornerPoints(points)
			c.cornerStepsReady = true
			c.projectorMu.Unlock()
			return nil
		},
		func() error {
			for _, mode := range projectionModes {
				out, err := c.callSetting(projectorTypeURI, mode.query)
				if err != nil {
					return err
				}
				active, err := bundleBool(out, mode.query)
				if err != nil {
					return err
				}
				if active {
					state.Mode = mode.name
					return nil
				}
			}
			return fmt.Errorf("deviceio: projector returned no active projection mode")
		},
	}
	for i := range featureKeys {
		i := i
		jobs = append(jobs, func() error {
			out, err := c.callSetting(focusSettingsURI, "enabled", "key:s:"+featureKeys[i])
			if err != nil {
				return err
			}
			featureEnabled[i], err = bundleBool(out, "key")
			return err
		})
	}

	jobCh := make(chan func() error)
	errCh := make(chan error, len(jobs))
	var wg sync.WaitGroup
	for range settingsReadWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				if err := job(); err != nil {
					errCh <- err
				}
			}
		}()
	}
	for _, job := range jobs {
		jobCh <- job
	}
	close(jobCh)
	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		return ProjectorSettings{}, err
	}

	for i, enabled := range featureEnabled {
		state.Features[featureKeys[i]] = enabled
	}
	return state, nil
}

func cloneCornerPoints(points map[string]CornerPoint) map[string]CornerPoint {
	clone := make(map[string]CornerPoint, len(points))
	for corner, point := range points {
		clone[corner] = point
	}
	return clone
}

func parseCornerGeometry(value string) (map[string]CornerPoint, [8]float64, error) {
	var raw [8]float64
	parts := strings.Split(value, ",")
	if len(parts) != len(raw) {
		return nil, raw, fmt.Errorf("deviceio: invalid keystone geometry %q", value)
	}
	for i, part := range parts {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, raw, fmt.Errorf("deviceio: invalid keystone coordinate %q: %w", part, err)
		}
		raw[i] = parsed
	}
	xStep, yStep := projectorWidth/200, projectorHeight/100
	points := map[string]CornerPoint{
		"lb": {X: cornerStep(raw[0]/xStep, cornerMaxX), Y: cornerStep(raw[1]/yStep, cornerMaxY)},
		"rb": {X: cornerStep((projectorWidth-raw[2])/xStep, cornerMaxX), Y: cornerStep(raw[3]/yStep, cornerMaxY)},
		"rt": {X: cornerStep((projectorWidth-raw[4])/xStep, cornerMaxX), Y: cornerStep((projectorHeight-raw[5])/yStep, cornerMaxY)},
		"lt": {X: cornerStep(raw[6]/xStep, cornerMaxX), Y: cornerStep((projectorHeight-raw[7])/yStep, cornerMaxY)},
	}
	return points, raw, nil
}

func cornerStep(value float64, maximum int) int {
	return min(max(int(math.Round(value)), 0), maximum)
}

// SetCorner writes the complete hardware-compositor geometry in one cheap
// property update. The vendor nudge API launches an Android process for each
// individual step, which is too slow for a continuous drag gesture.
func (c *Controller) SetCorner(corner string, x, y int, final bool) error {
	if x < 0 || x > cornerMaxX || y < 0 || y > cornerMaxY {
		return fmt.Errorf("deviceio: corner position %d,%d outside 0..%d,0..%d", x, y, cornerMaxX, cornerMaxY)
	}
	if _, ok := cornerNudges[corner]; !ok {
		return fmt.Errorf("deviceio: unknown corner %q", corner)
	}
	c.projectorMu.Lock()
	defer c.projectorMu.Unlock()
	if !c.cornerStepsReady {
		points, err := c.propertyRun.CornerSteps()
		if err != nil {
			return err
		}
		c.cornerSteps = points
		c.cornerStepsReady = true
	}
	previous := c.cornerSteps[corner]
	value, err := c.propertyRun.Get(keystoneProperty)
	if err != nil {
		return err
	}
	_, raw, err := parseCornerGeometry(value)
	if err != nil {
		return err
	}
	deltaX := float64(x-previous.X) * (projectorWidth / 200)
	deltaY := float64(y-previous.Y) * (projectorHeight / 100)
	switch corner {
	case "lt":
		raw[6], raw[7] = raw[6]+deltaX, raw[7]-deltaY
	case "rt":
		raw[4], raw[5] = raw[4]-deltaX, raw[5]-deltaY
	case "rb":
		raw[2], raw[3] = raw[2]-deltaX, raw[3]+deltaY
	case "lb":
		raw[0], raw[1] = raw[0]+deltaX, raw[1]+deltaY
	}
	for i := range raw {
		maximum := projectorWidth
		if i%2 == 1 {
			maximum = projectorHeight
		}
		raw[i] = math.Min(math.Max(raw[i], 0), maximum)
	}
	parts := make([]string, len(raw))
	for i, coordinate := range raw {
		parts[i] = strconv.FormatFloat(coordinate, 'f', 3, 64)
	}
	if err := c.propertyRun.Set(keystoneProperty, strings.Join(parts, ",")); err != nil {
		return err
	}
	point := CornerPoint{X: x, Y: y}
	c.cornerSteps[corner] = point
	if final {
		return c.propertyRun.SetCornerSteps(corner, point)
	}
	return nil
}

func (c *Controller) readContentInt(uri, method, key string) (int, error) {
	out, err := c.callSetting(uri, method)
	if err != nil {
		return 0, err
	}
	return bundleInt(out, key)
}

// callSetting limits all vendor-provider calls, including reads and writes,
// to a concurrency the projector can sustain. Reads no longer hold the
// mutation lock for an entire multi-call refresh, so a user command waits for
// at most one provider call instead of the full settings scan.
func (c *Controller) callSetting(uri, method string, extras ...string) (string, error) {
	c.settingsSlots <- struct{}{}
	defer func() { <-c.settingsSlots }()
	return c.settingsRun.Call(uri, method, extras...)
}

func (c *Controller) SetZoom(percent int) error {
	if percent < 40 || percent > 100 {
		return fmt.Errorf("deviceio: zoom %d outside 40..100", percent)
	}
	c.projectorMu.Lock()
	defer c.projectorMu.Unlock()
	value, err := c.propertyRun.Get(keystoneProperty)
	if err != nil {
		return err
	}
	_, raw, err := parseCornerGeometry(value)
	if err != nil {
		return err
	}
	current := geometryPercentage(raw)
	if current <= 0 {
		return fmt.Errorf("deviceio: invalid current projection area %.2f", current)
	}
	factor := math.Sqrt(float64(percent) / current)
	centerX := (raw[0] + raw[2] + raw[4] + raw[6]) / 4
	centerY := (raw[1] + raw[3] + raw[5] + raw[7]) / 4
	for i := 0; i < len(raw); i += 2 {
		raw[i] = math.Min(math.Max(centerX+(raw[i]-centerX)*factor, 0), projectorWidth)
		raw[i+1] = math.Min(math.Max(centerY+(raw[i+1]-centerY)*factor, 0), projectorHeight)
	}
	parts := make([]string, len(raw))
	for i, coordinate := range raw {
		parts[i] = strconv.FormatFloat(coordinate, 'f', 3, 64)
	}
	return c.propertyRun.Set(keystoneProperty, strings.Join(parts, ","))
}

func geometryPercentage(raw [8]float64) float64 {
	width := math.Max(raw[2], raw[4]) - math.Min(raw[0], raw[6])
	height := math.Max(raw[5], raw[7]) - math.Min(raw[1], raw[3])
	return 100 * math.Abs(width*height) / (projectorWidth * projectorHeight)
}

func (c *Controller) SetKeystone(axis string, angle int) error {
	if angle < minKeystoneAngle || angle > maxKeystoneAngle {
		return fmt.Errorf("deviceio: keystone angle %d outside %d..%d", angle, minKeystoneAngle, maxKeystoneAngle)
	}
	method, extraKey := "", ""
	switch axis {
	case "horizontal":
		method, extraKey = "adjustHorizontal", "adjustHorizontal"
	case "vertical":
		method, extraKey = "adjustVertical", "adjustVertical"
	default:
		return fmt.Errorf("deviceio: unknown keystone axis %q", axis)
	}
	c.projectorMu.Lock()
	defer c.projectorMu.Unlock()
	_, err := c.callSetting(keystoneURI, method, fmt.Sprintf("%s:i:%d", extraKey, angle))
	return err
}

var cornerNudges = map[string]map[string]string{
	"lt": {"left": "leftRegulaByLtX", "right": "rightRegulaByLtX", "up": "upRegulaByLtY", "down": "downRegulaByLtY"},
	"rt": {"left": "leftRegulaByRtX", "right": "rightRegulaByRtX", "up": "upRegulaByRtY", "down": "downRegulaByRtY"},
	"rb": {"left": "leftRegulaByRbX", "right": "rightRegulaByRbX", "up": "upRegulaByRbY", "down": "downRegulaByRbY"},
	"lb": {"left": "leftRegulaByLbX", "right": "rightRegulaByLbX", "up": "upRegulaByLbY", "down": "downRegulaByLbY"},
}

func (c *Controller) NudgeCorner(corner, direction string) error {
	directions, ok := cornerNudges[corner]
	if !ok {
		return fmt.Errorf("deviceio: unknown corner %q", corner)
	}
	method, ok := directions[direction]
	if !ok {
		return fmt.Errorf("deviceio: unknown corner direction %q", direction)
	}
	c.projectorMu.Lock()
	defer c.projectorMu.Unlock()
	_, err := c.callSetting(keystoneURI, method)
	return err
}

func (c *Controller) ResetKeystone() error {
	c.projectorMu.Lock()
	defer c.projectorMu.Unlock()
	_, err := c.callSetting(keystoneURI, "reset")
	return err
}

func (c *Controller) SetProjectionMode(name string) error {
	for _, mode := range projectionModes {
		if mode.name == name {
			c.projectorMu.Lock()
			defer c.projectorMu.Unlock()
			_, err := c.callSetting(projectorTypeURI, mode.set)
			return err
		}
	}
	return fmt.Errorf("deviceio: unknown projection mode %q", name)
}

func (c *Controller) SetProjectionFeature(key string, enabled bool) error {
	allowed := false
	for _, candidate := range featureKeys {
		if key == candidate {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("deviceio: unknown projection feature %q", key)
	}
	method := "disable"
	if enabled {
		method = "enable"
	}
	c.projectorMu.Lock()
	defer c.projectorMu.Unlock()
	_, err := c.callSetting(focusSettingsURI, method, "key:s:"+key)
	return err
}

// StartFocus starts the motor in one of the two vendor-defined directions.
// Each call rearms a short watchdog. The browser refreshes it while held; if
// the release message or connection disappears, the motor still stops.
func (c *Controller) StartFocus(direction string) error {
	method := ""
	switch direction {
	case "near":
		method = "ccw"
	case "far":
		method = "cw"
	default:
		return fmt.Errorf("deviceio: unknown focus direction %q", direction)
	}
	c.focusMu.Lock()
	defer c.focusMu.Unlock()
	if _, err := c.callSetting(focusManualURI, method); err != nil {
		return err
	}
	if c.focusTimer != nil {
		c.focusTimer.Stop()
	}
	c.focusTimer = time.AfterFunc(focusWatchdog, func() { _ = c.StopFocus() })
	return nil
}

func (c *Controller) StopFocus() error {
	c.focusMu.Lock()
	defer c.focusMu.Unlock()
	if c.focusTimer != nil {
		c.focusTimer.Stop()
		c.focusTimer = nil
	}
	_, err := c.callSetting(focusManualURI, "stopCWOrCCW")
	return err
}
