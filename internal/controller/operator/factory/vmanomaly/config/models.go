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

var temporalEnvelopeSeasonalities = map[string]struct{}{
	"hod_smooth": {}, "hod_spiky": {}, "hod_plateau": {},
	"dow_smooth": {}, "dow_spiky": {}, "dow_plateau": {},
	"weekpart_plateau": {}, "month_smooth": {}, "month_plateau": {},
}

type commonModelParams struct {
	Class                 string                  `yaml:"class"`
	Queries               []string                `yaml:"queries,omitempty"`
	Schedulers            []string                `yaml:"schedulers,omitempty"`
	TTL                   *duration               `yaml:"ttl,omitempty"`
	ProvideSeries         []string                `yaml:"provide_series,omitempty"`
	DetectionDirection    modelDetectionDirection `yaml:"detection_direction,omitempty"`
	DataRange             []any                   `yaml:"data_range,omitempty"`
	MinDevFromExpected    any                     `yaml:"min_dev_from_expected,omitempty"`
	MinRelDevFromExpected any                     `yaml:"min_rel_dev_from_expected,omitempty"`
	Scale                 any                     `yaml:"scale,omitempty"`
	ClipPredictions       bool                    `yaml:"clip_predictions,omitempty"`
	ForecastAt            []duration              `yaml:"forecast_at,omitempty"`
	// ScoreOutsideDataRange is a pointer so an explicit 0.0 survives marshalling.
	ScoreOutsideDataRange *float64 `yaml:"anomaly_score_outside_data_range,omitempty"`
}

func (p commonModelParams) queries() []string {
	return p.Queries
}

func (p commonModelParams) addPrefix(prefix string) {
	for i := range p.Schedulers {
		p.Schedulers[i] = fmt.Sprintf("%s-%s", prefix, p.Schedulers[i])
	}
	for i := range p.Queries {
		p.Queries[i] = fmt.Sprintf("%s-%s", prefix, p.Queries[i])
	}
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
	addPrefix(string)
}

type model struct {
	anomalyModel
}

func (m *model) init(class string) error {
	var mdl anomalyModel
	switch class {
	case "model.auto.AutoTunedModel", "auto":
		mdl = new(autoTunedModel)
	case "model.prophet.ProphetModel", "prophet":
		mdl = new(prophetModel)
	case "model.zscore.ZscoreModel", "model.online.OnlineZscoreModel", "zscore", "zscore_online":
		mdl = new(onlineZScoreModel)
	case "model.holtwinters.HoltWinters", "holtwinters":
		mdl = new(holtWintersModel)
	case "model.mad.MADModel", "model.online.OnlineMADModel", "mad", "mad_online":
		mdl = new(onlineMadModel)
	case "model.rolling_quantile.RollingQuantileModel", "model.online.RollingQuantileModel", "rolling_quantile":
		mdl = new(rollingQuantileModel)
	case "model.online.OnlineQuantileModel", "quantile_online":
		mdl = new(onlineQuantileModel)
	case "model.std.StdModel", "model.online.StdModel", "std":
		mdl = new(stdModel)
	case "model.isolation_forest.IsolationForestModel", "isolation_forest", "isolation_forest_univariate":
		mdl = new(isolationForestModel)
	case "model.isolation_forest.IsolationForestMultivariateModel", "isolation_forest_multivariate":
		mdl = new(isolationForestMultivariateModel)
	case "model.online.TemporalEnvelopeModel", "temporal_envelope":
		mdl = new(temporalEnvelopeModel)
	case "model.online.TemporalEnvelopeMultivariateModel", "temporal_envelope_multivariate":
		mdl = new(temporalEnvelopeMultivariateModel)
	default:
		return fmt.Errorf("model class=%q is not supported", class)
	}
	m.anomalyModel = mdl
	return nil
}

var (
	_ yaml.Marshaler   = (*model)(nil)
	_ yaml.Unmarshaler = (*model)(nil)
)

// Validate validates raw config
func (m *model) Validate(data []byte) error {
	if err := yaml.Unmarshal(data, m); err != nil {
		return err
	}
	return m.validate()
}

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
	if err := m.init(h.Class); err != nil {
		return err
	}
	if err := unmarshal(m.anomalyModel); err != nil {
		return err
	}
	return nil
}

type onlineModel struct {
	// Decay is a pointer to distinguish "unset" (omitted, vmanomaly applies its default)
	// from an explicit value, which must be in the range (0, 1].
	Decay *float64 `yaml:"decay,omitempty"`
	// HistoryStrength controls how strongly fitted history resists subsequent
	// online updates. Unset values are defaulted by vmanomaly.
	HistoryStrength *float64 `yaml:"history_strength,omitempty"`
}

func (m *onlineModel) validate() error {
	// See https://docs.victoriametrics.com/anomaly-detection/components/models/#decay
	// Valid values are in the range (0, 1]; unset is allowed and defaulted by vmanomaly.
	if m.Decay != nil && (*m.Decay <= 0 || *m.Decay > 1) {
		return fmt.Errorf("decay must be in range (0, 1], got %f", *m.Decay)
	}
	if m.HistoryStrength != nil && *m.HistoryStrength <= 0 {
		return fmt.Errorf("history_strength must be greater than 0, got %f", *m.HistoryStrength)
	}
	return nil
}

type autoTunedModel struct {
	commonModelParams  `yaml:",inline"`
	TunedClassName     string                      `yaml:"tuned_class_name"`
	OptimizationParams autoTunedOptimizationParams `yaml:"optimization_params,omitempty"`
}

func (m *autoTunedModel) validate() error {
	return m.OptimizationParams.validate()
}

type autoTunedOptimizationParams struct {
	AnomalyPercentage       *float64      `yaml:"anomaly_percentage"`
	OptimizedBusinessParams []string      `yaml:"optimized_business_params,omitempty"`
	FrozenParams            yaml.MapSlice `yaml:"frozen_params,omitempty"`
	Seed                    *int          `yaml:"seed,omitempty"`
	Splits                  *int          `yaml:"n_splits,omitempty"`
	TrainValRatio           *float64      `yaml:"train_val_ratio,omitempty"`
	ValidationScheme        string        `yaml:"validation_scheme,omitempty"`
	Beta                    *float64      `yaml:"beta,omitempty"`
	Exact                   bool          `yaml:"exact,omitempty"`
	OptimizeComplexity      *bool         `yaml:"optimize_complexity,omitempty"`
	Trials                  *int          `yaml:"n_trials,omitempty"`
	Timeout                 *float64      `yaml:"timeout,omitempty"`
	Jobs                    *int          `yaml:"n_jobs,omitempty"`
	ShowProgressBar         bool          `yaml:"show_progress_bar,omitempty"`
	GCAfterTrial            bool          `yaml:"gc_after_trial,omitempty"`
}

func (p *autoTunedOptimizationParams) validate() error {
	if p.AnomalyPercentage == nil {
		return fmt.Errorf("anomaly_percentage is required")
	}
	if *p.AnomalyPercentage < 0 || *p.AnomalyPercentage >= 0.5 {
		return fmt.Errorf("anomaly_percentage must be in range [0, 0.5), got %f", *p.AnomalyPercentage)
	}
	if p.Splits != nil && *p.Splits < 1 {
		return fmt.Errorf("n_splits must be positive, got %d", *p.Splits)
	}
	if p.Trials != nil && *p.Trials < 1 {
		return fmt.Errorf("n_trials must be positive, got %d", *p.Trials)
	}
	if p.TrainValRatio != nil && *p.TrainValRatio <= 0 {
		return fmt.Errorf("train_val_ratio must be greater than 0, got %f", *p.TrainValRatio)
	}
	if p.ValidationScheme != "" && p.ValidationScheme != "regular" && p.ValidationScheme != "leaky" {
		return fmt.Errorf("validation_scheme must be regular or leaky, got %q", p.ValidationScheme)
	}
	if p.Beta != nil && *p.Beta < 0 {
		return fmt.Errorf("beta must be non-negative, got %f", *p.Beta)
	}
	if p.Timeout != nil && *p.Timeout <= 0 {
		return fmt.Errorf("timeout must be greater than 0 seconds, got %f", *p.Timeout)
	}
	if p.Jobs != nil && *p.Jobs != -1 && *p.Jobs < 1 {
		return fmt.Errorf("n_jobs must be -1 or a positive integer, got %d", *p.Jobs)
	}
	for _, item := range p.FrozenParams {
		if item.Key == "class" || item.Key == "class_name" {
			return fmt.Errorf("frozen_params cannot contain reserved key %q", item.Key)
		}
	}
	return nil
}

type holtWintersModel struct {
	commonModelParams `yaml:",inline"`
	Frequency         *duration     `yaml:"frequency,omitempty"`
	Seasonality       *duration     `yaml:"seasonality"`
	Threshold         float64       `yaml:"z_threshold,omitempty"`
	Args              yaml.MapSlice `yaml:"args,omitempty"`
}

func (m *holtWintersModel) validate() error {
	return nil
}

type isolationForestModel struct {
	commonModelParams `yaml:",inline"`
	// Contamination is a float (e.g. 0.01) or the string "auto"; vmanomaly accepts both.
	Contamination    any           `yaml:"contamination,omitempty"`
	SeasonalFeatures []string      `yaml:"seasonal_features,omitempty"`
	Args             yaml.MapSlice `yaml:"args,omitempty"`
}

func (m *isolationForestModel) validate() error {
	return nil
}

type isolationForestMultivariateModel struct {
	commonModelParams `yaml:",inline"`
	// Contamination is a float (e.g. 0.01) or the string "auto"; vmanomaly accepts both.
	Contamination    any           `yaml:"contamination,omitempty"`
	SeasonalFeatures []string      `yaml:"seasonal_features,omitempty"`
	Args             yaml.MapSlice `yaml:"args,omitempty"`
	GroupBy          []string      `yaml:"groupby,omitempty"`
}

func (m *isolationForestMultivariateModel) validate() error {
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
	MinSubseason      string    `yaml:"min_subseason,omitempty"`
	UseTransform      bool      `yaml:"use_transform,omitempty"`
	GlobalSmoothing   float64   `yaml:"global_smoothing,omitempty"`
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
	Threshold         float64       `yaml:"z_threshold,omitempty"`
	MinSamplesSeen    int           `yaml:"min_n_samples_seen,omitempty"`
	Args              yaml.MapSlice `yaml:"args,omitempty"`
}

func (m *onlineZScoreModel) validate() error {
	if err := m.onlineModel.validate(); err != nil {
		return err
	}
	return nil
}

type prophetModel struct {
	commonModelParams     `yaml:",inline"`
	Seasonalities         []yaml.MapSlice `yaml:"seasonalities,omitempty"`
	TZSeasonalities       []tzSeasonality `yaml:"tz_seasonalities,omitempty"`
	TZAware               bool            `yaml:"tz_aware,omitempty"`
	TZUseCyclicalEncoding bool            `yaml:"tz_use_cyclical_encoding,omitempty"`
	Compression           yaml.MapSlice   `yaml:"compression,omitempty"`
	Args                  yaml.MapSlice   `yaml:"args,omitempty"`
}

func (m *prophetModel) validate() error {
	return nil
}

// tzSeasonality is a single vmanomaly tz_seasonality entry, which may be either
// a plain string name (e.g. "dow") or an object with keys like name, fourier_order,
// mode, etc. Both forms are valid in vmanomaly.
type tzSeasonality struct {
	val any // string or yaml.MapSlice
}

var (
	_ yaml.Marshaler   = tzSeasonality{}
	_ yaml.Unmarshaler = (*tzSeasonality)(nil)
)

func (t *tzSeasonality) UnmarshalYAML(unmarshal func(any) error) error {
	var ms yaml.MapSlice
	if err := unmarshal(&ms); err == nil {
		t.val = ms
		return nil
	}
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	t.val = s
	return nil
}

func (t tzSeasonality) MarshalYAML() (any, error) {
	return t.val, nil
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
	MinSamplesSeen    *int    `yaml:"min_n_samples_seen,omitempty"`
}

func (m *stdModel) validate() error {
	return nil
}

type temporalEnvelopeModel struct {
	commonModelParams `yaml:",inline"`
	Args              yaml.MapSlice `yaml:"args,omitempty"`
	Quantiles         *[]float64    `yaml:"quantiles,omitempty"`
	IQRThreshold      *float64      `yaml:"iqr_threshold,omitempty"`
	Alpha             *float64      `yaml:"alpha,omitempty"`
	LossReactivity    *float64      `yaml:"loss_reactivity,omitempty"`
	MinSamplesSeen    *int          `yaml:"min_n_samples_seen,omitempty"`
	ChangepointWindow *int          `yaml:"changepoint_window,omitempty"`
	Seasonalities     *[]string     `yaml:"seasonalities,omitempty"`
	Holidays          yaml.MapSlice `yaml:"holidays,omitempty"`
}

func (m *temporalEnvelopeModel) validate() error {
	if len(m.Args) > 0 {
		return fmt.Errorf("temporal_envelope does not accept arbitrary args")
	}
	if m.Quantiles != nil && (len(*m.Quantiles) != 2 || (*m.Quantiles)[0] < 0 || (*m.Quantiles)[0] >= (*m.Quantiles)[1] || (*m.Quantiles)[1] > 1) {
		return fmt.Errorf("quantiles must contain 2 ordered values in [0, 1], got %v", *m.Quantiles)
	}
	if m.IQRThreshold != nil && *m.IQRThreshold < 0 {
		return fmt.Errorf("iqr_threshold must be non-negative, got %f", *m.IQRThreshold)
	}
	if m.Alpha != nil && (*m.Alpha <= 0 || *m.Alpha > 1) {
		return fmt.Errorf("alpha must be in range (0, 1], got %f", *m.Alpha)
	}
	if m.LossReactivity != nil && *m.LossReactivity <= 0 {
		return fmt.Errorf("loss_reactivity must be greater than 0, got %f", *m.LossReactivity)
	}
	if m.MinSamplesSeen != nil && *m.MinSamplesSeen < 1 {
		return fmt.Errorf("min_n_samples_seen must be positive, got %d", *m.MinSamplesSeen)
	}
	if m.ChangepointWindow != nil && *m.ChangepointWindow < 1 {
		return fmt.Errorf("changepoint_window must be positive, got %d", *m.ChangepointWindow)
	}
	if m.Seasonalities == nil {
		return nil
	}
	seen := make(map[string]bool, len(*m.Seasonalities))
	for _, seasonality := range *m.Seasonalities {
		if _, ok := temporalEnvelopeSeasonalities[seasonality]; !ok {
			return fmt.Errorf("unsupported temporal_envelope seasonality %q", seasonality)
		}
		if seen[seasonality] {
			return fmt.Errorf("duplicate temporal_envelope seasonality %q", seasonality)
		}
		seen[seasonality] = true
	}
	return nil
}

type temporalEnvelopeMultivariateModel struct {
	temporalEnvelopeModel  `yaml:",inline"`
	GroupBy                []string `yaml:"groupby,omitempty"`
	DependencyRank         *int     `yaml:"dependency_rank,omitempty"`
	ScoreAggregation       string   `yaml:"score_aggregation,omitempty"`
	MaxChannels            *int     `yaml:"max_channels,omitempty"`
	RecommendedMaxChannels *int     `yaml:"recommended_max_channels,omitempty"`
	RandomState            *int     `yaml:"random_state,omitempty"`
}

func (m *temporalEnvelopeMultivariateModel) validate() error {
	if err := m.temporalEnvelopeModel.validate(); err != nil {
		return err
	}
	if m.DependencyRank != nil && *m.DependencyRank < 0 {
		return fmt.Errorf("dependency_rank must be non-negative, got %d", *m.DependencyRank)
	}
	if m.ScoreAggregation != "" && m.ScoreAggregation != "max" && m.ScoreAggregation != "mean" && m.ScoreAggregation != "l2" {
		return fmt.Errorf("score_aggregation must be max, mean, or l2, got %q", m.ScoreAggregation)
	}
	if m.MaxChannels != nil && *m.MaxChannels < 1 {
		return fmt.Errorf("max_channels must be positive, got %d", *m.MaxChannels)
	}
	if m.RecommendedMaxChannels != nil && *m.RecommendedMaxChannels < 1 {
		return fmt.Errorf("recommended_max_channels must be positive, got %d", *m.RecommendedMaxChannels)
	}
	if m.MaxChannels != nil && m.RecommendedMaxChannels != nil && *m.RecommendedMaxChannels > *m.MaxChannels {
		return fmt.Errorf("recommended_max_channels must not exceed max_channels")
	}
	return nil
}
