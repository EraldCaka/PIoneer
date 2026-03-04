package pwm

import (
	"os"
	"testing"

	"github.com/EraldCaka/PIoneer/pkg/config"
)

func newTestHandler() *pwmHandler {
	pins := []config.PWM{
		{Id: "pwm-fan", Pin: 18, FrequencyHz: 1000, DutyCycle: 0},
		{Id: "pwm-led", Pin: 12, FrequencyHz: 500, DutyCycle: 50},
	}
	return New(pins, "192.168.1.98", "alesio1234", "22").(*pwmHandler)
}

func TestNew_PinMapBuilt(t *testing.T) {
	h := newTestHandler()
	if len(h.pinMap) != 2 {
		t.Errorf("expected 2 pins, got %d", len(h.pinMap))
	}
	if _, ok := h.pinMap[18]; !ok {
		t.Error("pin 18 not found in map")
	}
	if _, ok := h.pinMap[12]; !ok {
		t.Error("pin 12 not found in map")
	}
}

func TestSet_InvalidDutyTooHigh(t *testing.T) {
	h := newTestHandler()
	err := h.Set(18, 101.0)
	if err == nil {
		t.Error("expected error for duty > 100")
	}
}

func TestSet_InvalidDutyNegative(t *testing.T) {
	h := newTestHandler()
	err := h.Set(18, -1.0)
	if err == nil {
		t.Error("expected error for duty < 0")
	}
}

func TestSet_UnconfiguredPin(t *testing.T) {
	h := newTestHandler()
	err := h.Set(99, 50.0)
	if err == nil {
		t.Error("expected error for unconfigured pin")
	}
}

func TestGet_UnconfiguredPin(t *testing.T) {
	h := newTestHandler()
	_, err := h.Get(99)
	if err == nil {
		t.Error("expected error for unconfigured pin")
	}
}

func TestStop_UnconfiguredPin(t *testing.T) {
	h := newTestHandler()
	err := h.Stop(99)
	if err == nil {
		t.Error("expected error for unconfigured pin")
	}
}

func TestDutyCycleRange(t *testing.T) {
	cases := []struct {
		duty    float64
		wantErr bool
	}{
		{0.0, false},
		{50.0, false},
		{100.0, false},
		{-0.1, true},
		{100.1, true},
	}
	h := newTestHandler()
	for _, tc := range cases {
		err := h.Set(18, tc.duty)
		if tc.wantErr && err == nil {
			t.Errorf("duty=%.1f: expected error, got nil", tc.duty)
		}
		if !tc.wantErr && err != nil {
			if err.Error() == "duty cycle must be between 0 and 100" {
				t.Errorf("duty=%.1f: unexpected validation error", tc.duty)
			}
		}
	}
}

func TestIntegration_PWM_SetGet(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("skipping integration test, set INTEGRATION=1 to run")
	}
	h := newTestHandler()

	if err := h.Set(18, 50.0); err != nil {
		t.Fatalf("Set(18, 50) failed: %v", err)
	}

	duty, err := h.Get(18)
	if err != nil {
		t.Fatalf("Get(18) failed: %v", err)
	}
	t.Logf("PWM pin 18 duty cycle: %.2f%%", duty)
}

func TestIntegration_PWM_Stop(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("skipping integration test, set INTEGRATION=1 to run")
	}
	h := newTestHandler()

	if err := h.Set(18, 75.0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := h.Stop(18); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if h.pinMap[18].DutyCycle != 0 {
		t.Errorf("expected duty 0 after stop, got %.2f", h.pinMap[18].DutyCycle)
	}
}

func TestIntegration_PWM_SetMultipleDuties(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("skipping integration test, set INTEGRATION=1 to run")
	}
	h := newTestHandler()
	duties := []float64{0, 25, 50, 75, 100}
	for _, d := range duties {
		if err := h.Set(18, d); err != nil {
			t.Errorf("Set(18, %.0f) failed: %v", d, err)
		} else {
			t.Logf("PWM pin 18 set to %.0f%%", d)
		}
	}
}
