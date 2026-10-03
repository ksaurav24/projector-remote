package deviceio

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeContentCaller struct {
	mu      sync.Mutex
	calls   []string
	results map[string]string
}

type fakePropertyCaller struct {
	mu    sync.Mutex
	value string
	steps map[string]CornerPoint
}

func (f *fakePropertyCaller) CornerSteps() (map[string]CornerPoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneCornerPoints(f.steps), nil
}

func (f *fakePropertyCaller) SetCornerSteps(corner string, point CornerPoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps[corner] = point
	return nil
}

func (f *fakePropertyCaller) Get(string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.value, nil
}

func (f *fakePropertyCaller) Set(_ string, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.value = value
	return nil
}

func (f *fakeContentCaller) Call(uri, method string, extras ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+":"+strings.Join(extras, ":"))
	if out, ok := f.results[method]; ok {
		return out, nil
	}
	return "Result: Bundle[{}]", nil
}

func projectorController(caller contentCaller) *Controller {
	c := newController(&fakeEventWriter{}, &fakeEventWriter{})
	c.settingsRun = caller
	c.propertyRun = &fakePropertyCaller{
		value: "0,0,1920,0,1920,1080,0,1080",
		steps: map[string]CornerPoint{"lt": {}, "rt": {}, "rb": {}, "lb": {}},
	}
	return c
}

func TestBundleValues(t *testing.T) {
	if got, err := bundleInt("Result: Bundle[{key=52.668926}]", "key"); err != nil || got != 53 {
		t.Fatalf("bundleInt = %d, %v; want 53", got, err)
	}
	if got, err := bundleBool("Result: Bundle[{enabled=true}]", "enabled"); err != nil || !got {
		t.Fatalf("bundleBool = %v, %v; want true", got, err)
	}
}

func TestProjectorSettingsReadsVendorState(t *testing.T) {
	results := map[string]string{
		"readCurrentScreenPercentage": "Result: Bundle[{key=52.668926}]",
		"readMinScreenPercentage":     "Result: Bundle[{key=40.0}]",
		"angleByHorizontal":           "Result: Bundle[{angleByHorizontal=-4}]",
		"angleByVertical":             "Result: Bundle[{angleByVertical=7}]",
		"isOrthographicForward":       "Result: Bundle[{isOrthographicForward=false}]",
		"isOrthographicRear":          "Result: Bundle[{isOrthographicRear=true}]",
		"isHoistingForward":           "Result: Bundle[{isHoistingForward=false}]",
		"isHoistingRear":              "Result: Bundle[{isHoistingRear=false}]",
		"enabled":                     "Result: Bundle[{key=true}]",
	}
	c := projectorController(&fakeContentCaller{results: results})
	got, err := c.ProjectorSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Zoom != 53 || got.MinZoom != 40 || got.Horizontal != -4 || got.Vertical != 7 || got.Mode != "rear" {
		t.Fatalf("unexpected settings: %+v", got)
	}
	for _, key := range featureKeys {
		if !got.Features[key] {
			t.Errorf("feature %s not enabled", key)
		}
	}
	if got.CornerMaxX != 64 || got.CornerMaxY != 36 || got.Corners["lt"] != (CornerPoint{}) {
		t.Errorf("unexpected corner state: %+v", got)
	}
}

func TestCornerGeometryAndAbsoluteWrite(t *testing.T) {
	points, _, err := parseCornerGeometry("96,108,1824,216,1728,756,288,864")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]CornerPoint{
		"lb": {X: 10, Y: 10}, "rb": {X: 10, Y: 20},
		"rt": {X: 20, Y: 30}, "lt": {X: 30, Y: 20},
	}
	for corner, expected := range want {
		if points[corner] != expected {
			t.Fatalf("%s = %v, want %v", corner, points[corner], expected)
		}
	}

	c := projectorController(&fakeContentCaller{results: map[string]string{}})
	property := c.propertyRun.(*fakePropertyCaller)
	if err := c.SetCorner("rt", 12, 8, true); err != nil {
		t.Fatal(err)
	}
	property.mu.Lock()
	value := property.value
	property.mu.Unlock()
	if value != "0.000,0.000,1920.000,0.000,1804.800,993.600,0.000,1080.000" {
		t.Fatalf("geometry = %q", value)
	}
	if err := c.SetCorner("rt", 65, 0, true); err == nil {
		t.Fatal("accepted corner position past native X limit")
	}
}

func TestZoomWritesGeometryDirectly(t *testing.T) {
	c := projectorController(&fakeContentCaller{results: map[string]string{}})
	if err := c.SetZoom(81); err != nil {
		t.Fatal(err)
	}
	property := c.propertyRun.(*fakePropertyCaller)
	property.mu.Lock()
	value := property.value
	property.mu.Unlock()
	if value != "96.000,54.000,1824.000,54.000,1824.000,1026.000,96.000,1026.000" {
		t.Fatalf("zoom geometry = %q", value)
	}
	_, raw, err := parseCornerGeometry(value)
	if err != nil {
		t.Fatal(err)
	}
	if got := geometryPercentage(raw); math.Abs(got-81) > 0.01 {
		t.Fatalf("geometry percentage = %.3f, want 81", got)
	}
}

func TestProjectorMutationsAreAllowListedAndMapped(t *testing.T) {
	fake := &fakeContentCaller{results: map[string]string{}}
	c := projectorController(fake)

	if err := c.SetZoom(39); err == nil {
		t.Fatal("SetZoom accepted a value below the device minimum")
	}
	if err := c.SetZoom(66); err != nil {
		t.Fatal(err)
	}
	if err := c.SetKeystone("diagonal", 0); err == nil {
		t.Fatal("SetKeystone accepted an unknown axis")
	}
	if err := c.SetKeystone("vertical", 41); err == nil {
		t.Fatal("SetKeystone accepted an out-of-range angle")
	}
	if err := c.NudgeCorner("lt", "right"); err != nil {
		t.Fatal(err)
	}
	if err := c.NudgeCorner("middle", "right"); err == nil {
		t.Fatal("NudgeCorner accepted an unknown corner")
	}
	if err := c.SetProjectionMode("rear"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProjectionFeature("anything", true); err == nil {
		t.Fatal("SetProjectionFeature accepted an unknown key")
	}

	want := []string{
		"rightRegulaByLtX:",
		"orthographicRear:",
	}
	if fmt.Sprint(fake.calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
}

func TestFocusWatchdogStopsMotor(t *testing.T) {
	fake := &fakeContentCaller{results: map[string]string{}}
	c := projectorController(fake)
	if err := c.StartFocus("near"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(focusWatchdog + 150*time.Millisecond)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) < 2 || fake.calls[0] != "ccw:" || fake.calls[len(fake.calls)-1] != "stopCWOrCCW:" {
		t.Fatalf("focus calls = %v", fake.calls)
	}
}
