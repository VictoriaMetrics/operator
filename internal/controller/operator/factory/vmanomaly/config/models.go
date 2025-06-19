package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v2"
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

type anomalyModel interface {
	validatable
	schedulers() []string
	queries() []string
}

type model struct {
	anomalyModel
}

var (
	_ yaml.Marshaler   = (*model)(nil)
	_ yaml.Unmarshaler = (*model)(nil)
)

// MarshalYAML implements yaml.Marshaller interface
func (m *model) MarshalYAML() (any, error) {
	return m.anomalyModel, nil
}

// UnmarshalYAML implements yaml.Unmarshaler interface
func (m *model) UnmarshalYAML(unmarshal func(any) error) error {
	var h header
	if err := unmarshal(&h); err != nil {
		return err
	}
	var mdl anomalyModel
	switch h.Class {
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
		return fmt.Errorf("model class=%q is not supported", h.Class)
	}
	if err := unmarshal(mdl); err != nil {
		return err
	}
	m.anomalyModel = mdl
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
	Threshold         float64 `yaml:"threshold,omitempty"`
	MinSamplesSeen    int     `yaml:"min_n_samples_seen,omitempty"`
	Compression       int     `yaml:"compression,omitempty"`
}

func (m *onlineMadModel) validate() error {
	return nil
}

type onlineQuantileModel struct {
	commonModelParams `yaml:",inline"`
	Quantiles         []float64 `yaml:"quantiles,omitempty"`
	SeasonalInterval  *duration `yaml:"seasonal_interval,omitempty"`
	MinSubseason      string    `yaml:"min_subseason"`
	UseTransform      bool      `yaml:"use_transform,omitempty"`
	GlobalSmoothing   float64   `yaml:"global_smooth,omitempty"`
	Scale             float64   `yaml:"scale,omitempty"`
	SeasonStartsFrom  time.Time `yaml:"season_starts_from,omitempty"`
	MinSamplesSeen    int       `yaml:"min_n_samples_seen,omitempty"`
	Compression       int       `yaml:"compression,omitempty"`
}

func (m *onlineQuantileModel) validate() error {
	return nil
}

type onlineZScoreModel struct {
	commonModelParams `yaml:",inline"`
	Threshold         float64 `yaml:"threshold,omitempty"`
	MinSamplesSeen    int     `yaml:"min_n_samples_seen,omitempty"`
}

func (m *onlineZScoreModel) validate() error {
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
