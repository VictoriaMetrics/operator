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

type modelDetectionDirection string

const (
	bothModelDirection          modelDetectionDirection = "both"           //nolint:unused
	aboveExpectedModelDirection modelDetectionDirection = "above_expected" //nolint:unused
	belowExpectedModelDirection modelDetectionDirection = "below_expected" //nolint:unused
)

type commonModelParams struct {
	Class                 string                  `yaml:"class"`
	Queries               []string                `yaml:"queries,omitempty"`
	Schedulers            []string                `yaml:"schedulers,omitempty"`
	ProvideSeries         []string                `yaml:"provide_series,omitempty"`
	DetectionDirection    modelDetectionDirection `yaml:"detection_direction,omitempty"`
	MinDevFromExpected    float64                 `yaml:"min_dev_from_expected,omitempty"`
	GroupBy               []string                `yaml:"groupby,omitempty"`
	Scale                 []float64               `yaml:"scale,omitempty"`
	ClipPredictions       bool                    `yaml:"clip_predictions,omitempty"`
	ScoreOutsideDataRange float64                 `yaml:"anomaly_score_outside_data_range,omitempty"`
}

func (p commonModelParams) queries() []string {
	return p.Queries
}

func (p commonModelParams) schedulers() []string {
	return p.Schedulers
}

func (p *commonModelParams) setClass(class string) {
	p.Class = class
}

type anomalyModel interface {
	validatable
	setClass(string)
	schedulers() []string
	queries() []string
}

type Model struct {
	anomalyModel
}

func (m *Model) init(class string) error {
	var mdl anomalyModel
	switch class {
	case "model.auto.AutoTunedModel", "auto":
		mdl = new(autoTunedModel)
	case "model.prophet.ProphetModel", "prophet":
		mdl = new(prophetModel)
	case "model.zscore.ZscoreModel", "zscore":
		mdl = new(zScoreModel)
	case "model.online.OnlineZscoreModel", "zscore_online":
		mdl = new(onlineZScoreModel)
	case "model.holtwinters.HoltWinters", "holtwinters":
		mdl = new(holtWintersModel)
	case "model.mad.MADModel", "mad":
		mdl = new(madModel)
	case "model.online.OnlineMADModel", "mad_online":
		mdl = new(onlineMadModel)
	case "model.rolling_quantile.RollingQuantileModel", "rolling_quantile":
		mdl = new(rollingQuantileModel)
	case "model.online.OnlineQuantileModel", "quantile_online":
		mdl = new(onlineQuantileModel)
	case "model.std.StdModel", "std":
		mdl = new(stdModel)
	case "model.isolation_forest.IsolationForestModel", "isolation_forest":
		mdl = new(isolationForestModel)
	case "model.isolation_forest.IsolationForestMultivariateModel", "isolation_forest_multivariate":
		mdl = new(isolationForestMultivariateModel)
	default:
		return fmt.Errorf("model class=%q is not supported", class)
	}
	m.anomalyModel = mdl
	return nil
}

var (
	_ yaml.Marshaler   = (*Model)(nil)
	_ yaml.Unmarshaler = (*Model)(nil)
)

// Validate validates raw config
func (m *Model) Validate(data []byte) error {
	if err := yaml.Unmarshal(data, m); err != nil {
		return err
	}
	return m.validate()
}

func modelFromSpec(spec *vmv1.VMAnomalyModelSpec) (*Model, error) {
	var m Model
	if err := m.init(spec.Class); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(spec.Params.Raw, m.anomalyModel); err != nil {
		return nil, err
	}
	m.setClass(spec.Class)
	return &m, nil
}

// MarshalYAML implements yaml.Marshaller interface
func (m *Model) MarshalYAML() (any, error) {
	return m.anomalyModel, nil
}

// UnmarshalYAML implements yaml.Unmarshaler interface
func (m *Model) UnmarshalYAML(unmarshal func(any) error) error {
	var h header
	if err := unmarshal(&h); err != nil {
		return err
	}
	if err := m.init(h.Class); err != nil {
		return err
	}
	if err := unmarshal(m.anomalyModel); err != nil {
		return err
	}
	return nil
}

type onlineModel struct {
	Decay float64 `yaml:"decay,omitempty"`
}

func (m *onlineModel) validate() error {
	// See https://docs.victoriametrics.com/anomaly-detection/components/models/#decay
	// Valid values are in the range [0, 1].
	if m.Decay < 0 || m.Decay > 1 {
		return fmt.Errorf("decay must be in range [0, 1], got %f", m.Decay)
	}
	return nil
}

type autoTunedModel struct {
	commonModelParams  `yaml:",inline"`
	TunedClassName     string                      `yaml:"tuned_class_name"`
	OptimizationParams autoTunedOptimizationParams `yaml:"optimization_params,omitempty"`
}

func (m *autoTunedModel) validate() error {
	return nil
}

type autoTunedOptimizationParams struct {
	AnomalyPercentage       float64   `yaml:"anomaly_percentage"`
	OptimizedBusinessParams []string  `yaml:"optimized_business_params,omitempty"`
	Seed                    int       `yaml:"seed,omitempty"`
	Splits                  int       `yaml:"n_splits,omitempty"`
	Trails                  int       `yaml:"n_trails,omitempty"`
	Timeout                 *duration `yaml:"timeout,omitempty"`
}

type holtWintersModel struct {
	commonModelParams `yaml:",inline"`
	Frequency         *duration      `yaml:"frequency,omitempty"`
	Seasonality       *duration      `yaml:"seasonality"`
	Threshold         float64        `yaml:"z_threshold,omitempty"`
	Args              map[string]any `yaml:"args,omitempty"`
}

func (m *holtWintersModel) validate() error {
	return nil
}

type isolationForestModel struct {
	commonModelParams `yaml:",inline"`
	Contamination     string         `yaml:"contamination,omitempty"`
	SeasonalFeatures  []string       `yaml:"seasonal_features,omitempty"`
	Args              map[string]any `yaml:"args,omitempty"`
}

func (m *isolationForestModel) validate() error {
	return nil
}

type isolationForestMultivariateModel struct {
	commonModelParams `yaml:",inline"`
	Contamination     string         `yaml:"contamination,omitempty"`
	SeasonalFeatures  []string       `yaml:"seasonal_features,omitempty"`
	Args              map[string]any `yaml:"args,omitempty"`
}

func (m *isolationForestMultivariateModel) validate() error {
	return nil
}

type madModel struct {
	commonModelParams `yaml:",inline"`
	Threshold         float64 `yaml:"threshold,omitempty"`
}

func (m *madModel) validate() error {
	return nil
}

type onlineMadModel struct {
	commonModelParams `yaml:",inline"`
	onlineModel       `yaml:",inline"`
	Threshold         float64 `yaml:"threshold,omitempty"`
	MinSamplesSeen    int     `yaml:"min_n_samples_seen,omitempty"`
	Compression       int     `yaml:"compression,omitempty"`
}

func (m *onlineMadModel) validate() error {
	if err := m.onlineModel.validate(); err != nil {
		return err
	}
	return nil
}

type onlineQuantileModel struct {
	commonModelParams `yaml:",inline"`
	onlineModel       `yaml:",inline"`
	Quantiles         []float64 `yaml:"quantiles,omitempty"`
	SeasonalInterval  *duration `yaml:"seasonal_interval,omitempty"`
	MinSubseason      string    `yaml:"min_subseason"`
	UseTransform      bool      `yaml:"use_transform,omitempty"`
	GlobalSmoothing   float64   `yaml:"global_smooth,omitempty"`
	Scale             float64   `yaml:"scale,omitempty"`
	SeasonStartsFrom  time.Time `yaml:"season_starts_from,omitempty"`
	MinSamplesSeen    int       `yaml:"min_n_samples_seen,omitempty"`
	Compression       int       `yaml:"compression,omitempty"`
	IqrThreshold      float64   `yaml:"iqr_threshold,omitempty"`
}

func (m *onlineQuantileModel) validate() error {
	if err := m.onlineModel.validate(); err != nil {
		return err
	}
	return nil
}

type onlineZScoreModel struct {
	commonModelParams `yaml:",inline"`
	onlineModel       `yaml:",inline"`
	Threshold         float64 `yaml:"threshold,omitempty"`
	MinSamplesSeen    int     `yaml:"min_n_samples_seen,omitempty"`
}

func (m *onlineZScoreModel) validate() error {
	if err := m.onlineModel.validate(); err != nil {
		return err
	}
	return nil
}

type zScoreModel struct {
	commonModelParams `yaml:",inline"`
	Threshold         float64 `yaml:"z_threshold,omitempty"`
}

func (m *zScoreModel) validate() error {
	return nil
}

type prophetModel struct {
	commonModelParams     `yaml:",inline"`
	Seasonalities         *prophetModelSeasonality `yaml:"seasonality,omitempty"`
	TZSeasonalities       *prophetModelSeasonality `yaml:"tz_seasonality,omitempty"`
	Scale                 float64                  `yaml:"scale"`
	TZAware               bool                     `yaml:"tz_aware,omitempty"`
	TZUseCyclicalEncoding bool                     `yaml:"tz_use_cyclical_encoding,omitempty"`
}

func (m *prophetModel) validate() error {
	return nil
}

type prophetModelSeasonality struct {
	Name         string  `yaml:"name"`
	Period       float64 `yaml:"period"`
	FourierOrder int     `yaml:"fourier_order,omitempty"`
	PriorScale   int     `yaml:"prior_scale,omitempty"`
}

type rollingQuantileModel struct {
	commonModelParams `yaml:",inline"`
	Quantile          float64 `yaml:"quantile,omitempty"`
	WindowSteps       int     `yaml:"window_steps,omitempty"`
}

func (m *rollingQuantileModel) validate() error {
	return nil
}

type stdModel struct {
	commonModelParams `yaml:",inline"`
	Threshold         float64 `yaml:"z_threshold,omitempty"`
	Period            int     `yaml:"period,omitempty"`
}

func (m *stdModel) validate() error {
	return nil
}

func SelectModels(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) ([]*vmv1.VMAnomalyModel, error) {
	var selectedConfigs []*vmv1.VMAnomalyModel
	var namespacedNames []string
	opts := &k8stools.SelectorOpts{
		DefaultNamespace: cr.Namespace,
	}
	if cr.Spec.ModelSelector != nil {
		opts.ObjectSelector = cr.Spec.ModelSelector.ObjectSelector
		opts.NamespaceSelector = cr.Spec.ModelSelector.NamespaceSelector
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1.VMAnomalyModelList) {
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
	logger.SelectedObjects(ctx, "VMAnomalyModel", len(namespacedNames), 0, namespacedNames)

	return selectedConfigs, nil
}
