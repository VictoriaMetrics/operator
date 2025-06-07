package vmanomaly

import (
	"fmt"
	"time"

	"github.com/goccy/go-yaml"
)

type scheduler struct {
	Data validatable `yaml:",inline"`
}

func (s *scheduler) UnmarshalYAML(data []byte) error {
	h := struct {
		Class string `yaml:"class"`
	}{}
	if err := yaml.Unmarshal(data, &h); err != nil {
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
	if err := yaml.UnmarshalWithOptions(data, sch, yaml.Strict()); err != nil {
		return err
	}
	s.Data = sch
	return nil
}

func (s *scheduler) validate() error {
	return s.Data.validate()
}

type periodicScheduler struct {
	Class      string        `yaml:"class"`
	FitWindow  *duration     `yaml:"fit_window"`
	InferEvery *duration     `yaml:"infer_every"`
	FitEvery   *duration     `yaml:"fit_every,omitempty"`
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
	if !s.InferStartISO.IsZero() {
		inferStart = s.InferStartISO.Unix()
	} else if s.InferStartS != 0 {
		inferStart = s.InferStartS
	} else {
		return fmt.Errorf(`either "infer_start_iso" or "infer_start_s" should be set`)
	}
	var inferEnd int64
	if !s.InferEndISO.IsZero() {
		inferEnd = s.InferEndISO.Unix()
	} else if s.InferEndS != 0 {
		inferEnd = s.InferEndS
	} else {
		return fmt.Errorf(`either "infer_end_iso" or "infer_end_s" should be set`)
	}
	if inferStart >= inferEnd {
		return fmt.Errorf(`"infer_start_*" should be smaller than "infer_end_*"`)
	}
	var fitStart int64
	if !s.FitStartISO.IsZero() {
		fitStart = s.FitStartISO.Unix()
	} else if s.FitStartS != 0 {
		fitStart = s.FitStartS
	} else {
		return fmt.Errorf(`either "fit_start_iso" or "fit_start_s" should be set`)
	}
	var fitEnd int64
	if !s.FitEndISO.IsZero() {
		fitEnd = s.FitEndISO.Unix()
	} else if s.FitEndS != 0 {
		fitEnd = s.FitEndS
	} else {
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
	if !s.FromISO.IsZero() {
		from = s.FromISO.Unix()
	} else if s.FromS != 0 {
		from = s.FromS
	} else {
		return fmt.Errorf(`either "from_iso" or "from_s" should be set`)
	}
	var to int64
	if !s.ToISO.IsZero() {
		to = s.ToISO.Unix()
	} else if s.ToS != 0 {
		to = s.ToS
	} else {
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
