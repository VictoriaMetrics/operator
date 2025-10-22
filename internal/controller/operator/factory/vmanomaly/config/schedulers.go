package config

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/yaml.v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

type anomalyScheduler interface {
	validatable
	setClass(string)
}

type Scheduler struct {
	anomalyScheduler
}

var (
	_ yaml.Marshaler   = (*Scheduler)(nil)
	_ yaml.Unmarshaler = (*Scheduler)(nil)
)

// Validate validates raw config
func (s *Scheduler) Validate(data []byte) error {
	if err := yaml.Unmarshal(data, s); err != nil {
		return err
	}
	return s.validate()
}

// UnmarshalYAML implements yaml.Unmarshaler interface
func (s *Scheduler) UnmarshalYAML(unmarshal func(any) error) error {
	var h header
	if err := unmarshal(&h); err != nil {
		return err
	}
	if err := s.init(h.Class); err != nil {
		return err
	}
	if err := unmarshal(s.anomalyScheduler); err != nil {
		return err
	}
	return nil
}

func (s *Scheduler) init(class string) error {
	var sch anomalyScheduler
	switch class {
	case "scheduler.periodic.PeriodicScheduler", "periodic":
		sch = new(periodicScheduler)
	case "scheduler.oneoff.OneoffScheduler", "oneoff":
		sch = new(oneoffScheduler)
	case "scheduler.backtesting.BacktestingScheduler", "backtesting":
		sch = new(backtestingScheduler)
	default:
		return fmt.Errorf("anomaly scheduler class=%q is not supported", class)
	}
	s.anomalyScheduler = sch
	return nil
}

// MarshalYAML implements yaml.Marshaler interface
func (s *Scheduler) MarshalYAML() (any, error) {
	return s.anomalyScheduler, nil
}

func schedulerFromSpec(spec *vmv1.VMAnomalySchedulerSpec) (*Scheduler, error) {
	var s Scheduler
	if err := s.init(spec.Class); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(spec.Params.Raw, s.anomalyScheduler); err != nil {
		return nil, err
	}
	s.setClass(spec.Class)
	return &s, nil
}

type commonSchedulerParams struct {
	Class string `yaml:"class"`
}

func (p *commonSchedulerParams) setClass(class string) {
	p.Class = class
}

type noopScheduler struct {
	commonSchedulerParams `yaml:",inline"`
}

func (s *noopScheduler) validate() error {
	return nil
}

type periodicScheduler struct {
	commonSchedulerParams `yaml:",inline"`
	FitEvery              *duration     `yaml:"fit_every,omitempty"`
	FitWindow             *duration     `yaml:"fit_window"`
	InferEvery            *duration     `yaml:"infer_every"`
	StartFrom             time.Time     `yaml:"start_from,omitempty"`
	Timezone              time.Location `yaml:"tz,omitempty"`
}

func (s *periodicScheduler) validate() error {
	return nil
}

type oneoffScheduler struct {
	commonSchedulerParams `yaml:",inline"`
	InferStartISO         time.Time `yaml:"infer_start_iso,omitempty"`
	InferStartS           int64     `yaml:"infer_start_s,omitempty"`
	InferEndISO           time.Time `yaml:"infer_end_iso,omitempty"`
	InferEndS             int64     `yaml:"infer_end_s,omitempty"`
	FitStartISO           time.Time `yaml:"fit_start_iso"`
	FitStartS             int64     `yaml:"fit_start_s"`
	FitEndISO             time.Time `yaml:"fit_end_iso"`
	FitEndS               int64     `yaml:"fit_end_s"`
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

func (s *oneoffScheduler) setClass(class string) {
	s.Class = class
}

type backtestingScheduler struct {
	commonSchedulerParams `yaml:",inline"`
	FitWindow             *duration `yaml:"fit_window"`
	FromISO               time.Time `yaml:"from_iso"`
	FromS                 int64     `yaml:"from_s"`
	ToISO                 time.Time `yaml:"to_iso"`
	ToS                   int64     `yaml:"to_s"`
	FitEvery              *duration `yaml:"fit_every"`
	Jobs                  int       `yaml:"n_jobs,omitempty"`
	InferenceOnly         bool      `yaml:"inference_only,omitempty"`
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

func SelectSchedulers(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) ([]*vmv1.VMAnomalyScheduler, error) {
	var selectedConfigs []*vmv1.VMAnomalyScheduler
	var namespacedNames []string
	opts := &k8stools.SelectorOpts{
		DefaultNamespace: cr.Namespace,
	}
	if cr.Spec.SchedulerSelector != nil {
		opts.ObjectSelector = cr.Spec.SchedulerSelector.ObjectSelector
		opts.NamespaceSelector = cr.Spec.SchedulerSelector.NamespaceSelector
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1.VMAnomalySchedulerList) {
		for i := range list.Items {
			item := &list.Items[i]
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			rclient.Scheme().Default(item)
			namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			selectedConfigs = append(selectedConfigs, item)
		}
	}); err != nil {
		return nil, err
	}

	build.OrderByKeys(selectedConfigs, namespacedNames)
	logger.SelectedObjects(ctx, "VMAnomalyScheduler", len(namespacedNames), 0, namespacedNames)

	return selectedConfigs, nil
}
