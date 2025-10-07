package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v2"
)

type scheduler struct {
	validatable
}

var (
	_ yaml.Marshaler   = (*scheduler)(nil)
	_ yaml.Unmarshaler = (*scheduler)(nil)
)

// UnmarshalYAML implements yaml.Unmarshaller interface
func (s *scheduler) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var h header
	if err := unmarshal(&h); err != nil {
		return err
	}
	var sch validatable
	switch h.Class {
	case "scheduler.periodic.PeriodicScheduler", "periodic":
		sch = new(periodicScheduler)
	case "scheduler.oneoff.OneoffScheduler", "oneoff":
		sch = new(oneoffScheduler)
	case "scheduler.backtesting.BacktestingScheduler", "backtesting":
		sch = new(backtestingScheduler)
	default:
		return fmt.Errorf("anomaly scheduler class=%q is not supported", h.Class)
	}
	if err := unmarshal(sch); err != nil {
		return err
	}
	s.validatable = sch
	return nil
}

// MarshalYAML implements yaml.Marshaler interface
func (s *scheduler) MarshalYAML() (any, error) {
	return s.validatable, nil
}

type noopScheduler struct {
	Class string `yaml:"class"`
}

func (s *noopScheduler) validate() error {
	return nil
}

type periodicScheduler struct {
	Class      string        `yaml:"class"`
	FitEvery   *duration     `yaml:"fit_every,omitempty"`
	FitWindow  *duration     `yaml:"fit_window"`
	InferEvery *duration     `yaml:"infer_every"`
	StartFrom  time.Time     `yaml:"start_from,omitempty"`
	Timezone   time.Location `yaml:"tz,omitempty"`
}

func (s *periodicScheduler) validate() error {
	return nil
}

type oneoffScheduler struct {
	Class         string    `yaml:"class"`
	InferStartISO time.Time `yaml:"infer_start_iso,omitempty"`
	InferStartS   int64     `yaml:"infer_start_s,omitempty"`
	InferEndISO   time.Time `yaml:"infer_end_iso,omitempty"`
	InferEndS     int64     `yaml:"infer_end_s,omitempty"`
	FitStartISO   time.Time `yaml:"fit_start_iso"`
	FitStartS     int64     `yaml:"fit_start_s"`
	FitEndISO     time.Time `yaml:"fit_end_iso"`
	FitEndS       int64     `yaml:"fit_end_s"`
}

func (s *oneoffScheduler) validate() error {
	var inferStart int64
	switch {
	case !s.InferStartISO.IsZero():
		inferStart = s.InferStartISO.Unix()
	case s.InferStartS != 0:
		inferStart = s.InferStartS
	default:
		return fmt.Errorf(`either "infer_start_iso" or "infer_start_s" should be set`)
	}
	var inferEnd int64
	switch {
	case !s.InferEndISO.IsZero():
		inferEnd = s.InferEndISO.Unix()
	case s.InferEndS != 0:
		inferEnd = s.InferEndS
	default:
		return fmt.Errorf(`either "infer_end_iso" or "infer_end_s" should be set`)
	}
	if inferStart >= inferEnd {
		return fmt.Errorf(`"infer_start_*" should be smaller than "infer_end_*"`)
	}
	var fitStart int64
	switch {
	case !s.FitStartISO.IsZero():
		fitStart = s.FitStartISO.Unix()
	case s.FitStartS != 0:
		fitStart = s.FitStartS
	default:
		return fmt.Errorf(`either "fit_start_iso" or "fit_start_s" should be set`)
	}
	var fitEnd int64
	switch {
	case !s.FitEndISO.IsZero():
		fitEnd = s.FitEndISO.Unix()
	case s.FitEndS != 0:
		fitEnd = s.FitEndS
	default:
		return fmt.Errorf(`either "fit_end_iso" or "fit_end_s" should be set`)
	}
	if fitStart >= fitEnd {
		return fmt.Errorf(`"fit_start_*" should be smaller than "fit_end_*"`)
	}
	return nil
}

type backtestingScheduler struct {
	Class         string    `yaml:"class"`
	FitWindow     *duration `yaml:"fit_window"`
	FromISO       time.Time `yaml:"from_iso"`
	FromS         int64     `yaml:"from_s"`
	ToISO         time.Time `yaml:"to_iso"`
	ToS           int64     `yaml:"to_s"`
	FitEvery      *duration `yaml:"fit_every"`
	Jobs          int       `yaml:"n_jobs,omitempty"`
	InferenceOnly bool      `yaml:"inference_only,omitempty"`
}

func (s *backtestingScheduler) validate() error {
	var from int64
	switch {
	case !s.FromISO.IsZero():
		from = s.FromISO.Unix()
	case s.FromS != 0:
		from = s.FromS
	default:
		return fmt.Errorf(`either "from_iso" or "from_s" should be set`)
	}
	var to int64
	switch {
	case !s.ToISO.IsZero():
		to = s.ToISO.Unix()
	case s.ToS != 0:
		to = s.ToS
	default:
		return fmt.Errorf(`either "to_iso" or "to_s" should be set`)
	}
	if from >= to {
		return fmt.Errorf(`"from_*" should be smaller than "to_*"`)
	}
	if s.Jobs < 0 {
		return fmt.Errorf(`"n_jobs" should be positive`)
	}
	return nil
}
