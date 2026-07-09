// Package config is the public Go mirror of WebRTC AEC3's tuning
// surface — api/audio/echo_canceller3_config.h's EchoCanceller3Config,
// tracking WebRTC M131 field-for-field (the same upstream revision
// internal/aec3's port targets; see that package's own doc comments
// for what's ported vs. deliberately dropped, notably MultiChannel,
// a stereo-detection concern not exercised by this port and not
// present here).
//
// Config and its nested types are read directly by
// github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3's
// components; this package exists so a Config value (and
// DefaultConfig/Validate) can be constructed and validated from
// outside that internal package — see the root aec package's
// CancellerConfig.Tuning field, which is the only supported way to
// reach this package's types from application code.
//
// Field names and nesting mirror the upstream C++ struct's own
// grouping (Delay, Filter, Erle, Suppressor, ...); each field's doc
// comment names its upstream C++ counterpart tersely rather than
// re-deriving AEC3's tuning semantics from scratch — deep tuning
// (choosing non-default values with intent, rather than just
// wiring DefaultConfig() through) requires understanding AEC3's
// internals (delay estimation, adaptive filtering, nonlinear
// suppression) well enough to reason about the upstream
// documentation and source these fields are lifted from.
//
// # Stability
//
// This package's field set and semantics track upstream's; a future
// WebRTC revision bump could add, remove, or re-range fields to match.
// It is not expected to change independently of such a bump.
package config

// BufferingConfig is EchoCanceller3Config::Buffering.
type BufferingConfig struct {
	ExcessRenderDetectionIntervalBlocks int
	MaxAllowedExcessRenderBlocks        int
}

// DelaySelectionThresholds is
// EchoCanceller3Config::Delay::DelaySelectionThresholds.
type DelaySelectionThresholds struct {
	Initial   int
	Converged int
}

// AlignmentMixingConfig is
// EchoCanceller3Config::Delay::AlignmentMixing.
type AlignmentMixingConfig struct {
	Downmix                bool
	AdaptiveSelection      bool
	ActivityPowerThreshold float32
	PreferFirstTwoChannels bool
}

// DelayConfig is EchoCanceller3Config::Delay.
type DelayConfig struct {
	DefaultDelay                     int
	DownSamplingFactor               int
	NumFilters                       int
	DelayHeadroomSamples             int
	HysteresisLimitBlocks            int
	FixedCaptureDelaySamples         int
	DelayEstimateSmoothing           float32
	DelayEstimateSmoothingDelayFound float32
	DelayCandidateDetectionThreshold float32
	DelaySelectionThresholds         DelaySelectionThresholds
	UseExternalDelayEstimator        bool
	LogWarningOnDelayChanges         bool
	RenderAlignmentMixing            AlignmentMixingConfig
	CaptureAlignmentMixing           AlignmentMixingConfig
	DetectPreEcho                    bool
}

// RefinedFilterConfig is
// EchoCanceller3Config::Filter::RefinedConfiguration.
type RefinedFilterConfig struct {
	LengthBlocks     int
	LeakageConverged float32
	LeakageDiverged  float32
	ErrorFloor       float32
	ErrorCeil        float32
	NoiseGate        float32
}

// CoarseFilterConfig is
// EchoCanceller3Config::Filter::CoarseConfiguration.
type CoarseFilterConfig struct {
	LengthBlocks int
	Rate         float32
	NoiseGate    float32
}

// FilterConfig is EchoCanceller3Config::Filter.
type FilterConfig struct {
	Refined                       RefinedFilterConfig
	Coarse                        CoarseFilterConfig
	RefinedInitial                RefinedFilterConfig
	CoarseInitial                 CoarseFilterConfig
	ConfigChangeDurationBlocks    int
	InitialStateSeconds           float32
	CoarseResetHangoverBlocks     int
	ConservativeInitialPhase      bool
	EnableCoarseFilterOutputUsage bool
	UseLinearFilter               bool
	HighPassFilterEchoReference   bool
	ExportLinearAecOutput         bool
}

// EpStrengthConfig is EchoCanceller3Config::EpStrength.
type EpStrengthConfig struct {
	DefaultGain                            float32
	DefaultLen                             float32
	NearendLen                             float32
	EchoCanSaturate                        bool
	BoundedErl                             bool
	ErleOnsetCompensationInDominantNearend bool
	UseConservativeTailFrequencyResponse   bool
}

// RenderLevelsConfig is EchoCanceller3Config::RenderLevels.
type RenderLevelsConfig struct {
	ActiveRenderLimit            float32
	PoorExcitationRenderLimit    float32
	PoorExcitationRenderLimitDS8 float32
	RenderPowerGainDB            float32
}

// ErleConfig is EchoCanceller3Config::Erle.
type ErleConfig struct {
	Min                        float32
	MaxL                       float32
	MaxH                       float32
	OnsetDetection             bool
	NumSections                int
	ClampQualityEstimateToZero bool
	ClampQualityEstimateToOne  bool
}

// EchoAudibilityConfig is EchoCanceller3Config::EchoAudibility.
type EchoAudibilityConfig struct {
	LowRenderLimit                  float32
	NormalRenderLimit               float32
	FloorPower                      float32
	AudibilityThresholdLF           float32
	AudibilityThresholdMF           float32
	AudibilityThresholdHF           float32
	UseStationarityProperties       bool
	UseStationarityPropertiesAtInit bool
}

// EchoRemovalControlConfig is EchoCanceller3Config::EchoRemovalControl.
type EchoRemovalControlConfig struct {
	HasClockDrift           bool
	LinearAndStableEchoPath bool
}

// EchoModelConfig is EchoCanceller3Config::EchoModel.
type EchoModelConfig struct {
	NoiseFloorHold             int
	MinNoiseFloorPower         float32
	StationaryGateSlope        float32
	NoiseGatePower             float32
	NoiseGateSlope             float32
	RenderPreWindowSize        int
	RenderPostWindowSize       int
	ModelReverbInNonlinearMode bool
}

// ComfortNoiseConfig is EchoCanceller3Config::ComfortNoise.
type ComfortNoiseConfig struct {
	NoiseFloorDbfs float32
}

// MaskingThresholds is
// EchoCanceller3Config::Suppressor::MaskingThresholds.
type MaskingThresholds struct {
	EnrTransparent float32
	EnrSuppress    float32
	EmrTransparent float32
}

// SuppressorTuning is EchoCanceller3Config::Suppressor::Tuning.
type SuppressorTuning struct {
	MaskLF         MaskingThresholds
	MaskHF         MaskingThresholds
	MaxIncFactor   float32
	MaxDecFactorLF float32
}

// DominantNearendDetectionConfig is
// EchoCanceller3Config::Suppressor::DominantNearendDetection.
type DominantNearendDetectionConfig struct {
	EnrThreshold             float32
	EnrExitThreshold         float32
	SnrThreshold             float32
	HoldDuration             int
	TriggerThreshold         int
	UseDuringInitialPhase    bool
	UseUnboundedEchoSpectrum bool
}

// SubbandRegion is
// EchoCanceller3Config::Suppressor::SubbandNearendDetection::SubbandRegion.
type SubbandRegion struct {
	Low  int
	High int
}

// SubbandNearendDetectionConfig is
// EchoCanceller3Config::Suppressor::SubbandNearendDetection.
type SubbandNearendDetectionConfig struct {
	NearendAverageBlocks int
	Subband1             SubbandRegion
	Subband2             SubbandRegion
	NearendThreshold     float32
	SnrThreshold         float32
}

// HighBandsSuppressionConfig is
// EchoCanceller3Config::Suppressor::HighBandsSuppression.
type HighBandsSuppressionConfig struct {
	EnrThreshold                   float32
	MaxGainDuringEcho              float32
	AntiHowlingActivationThreshold float32
	AntiHowlingGain                float32
}

// SuppressorConfig is EchoCanceller3Config::Suppressor.
type SuppressorConfig struct {
	NearendAverageBlocks          int
	NormalTuning                  SuppressorTuning
	NearendTuning                 SuppressorTuning
	LFSmoothingDuringInitialPhase bool
	LastPermanentLFSmoothingBand  int
	LastLFSmoothingBand           int
	LastLFBand                    int
	FirstHFBand                   int
	DominantNearendDetection      DominantNearendDetectionConfig
	SubbandNearendDetection       SubbandNearendDetectionConfig
	UseSubbandNearendDetection    bool
	HighBandsSuppression          HighBandsSuppressionConfig
	FloorFirstIncrease            float32
	ConservativeHFSuppression     bool
}

// Config is the (currently partial) Go mirror of
// EchoCanceller3Config.
type Config struct {
	Buffering          BufferingConfig
	Delay              DelayConfig
	Filter             FilterConfig
	Erle               ErleConfig
	EpStrength         EpStrengthConfig
	EchoAudibility     EchoAudibilityConfig
	RenderLevels       RenderLevelsConfig
	EchoRemovalControl EchoRemovalControlConfig
	EchoModel          EchoModelConfig
	ComfortNoise       ComfortNoiseConfig
	Suppressor         SuppressorConfig
}

// DefaultConfig returns a Config populated with
// EchoCanceller3Config's in-class default member initializers.
func DefaultConfig() Config {
	return Config{
		Buffering: BufferingConfig{
			ExcessRenderDetectionIntervalBlocks: 250,
			MaxAllowedExcessRenderBlocks:        8,
		},
		Delay: DelayConfig{
			DefaultDelay:                     5,
			DownSamplingFactor:               4,
			NumFilters:                       5,
			DelayHeadroomSamples:             32,
			HysteresisLimitBlocks:            1,
			FixedCaptureDelaySamples:         0,
			DelayEstimateSmoothing:           0.7,
			DelayEstimateSmoothingDelayFound: 0.7,
			DelayCandidateDetectionThreshold: 0.2,
			DelaySelectionThresholds:         DelaySelectionThresholds{Initial: 5, Converged: 20},
			UseExternalDelayEstimator:        false,
			LogWarningOnDelayChanges:         false,
			RenderAlignmentMixing: AlignmentMixingConfig{
				Downmix: false, AdaptiveSelection: true, ActivityPowerThreshold: 10000, PreferFirstTwoChannels: true,
			},
			CaptureAlignmentMixing: AlignmentMixingConfig{
				Downmix: false, AdaptiveSelection: true, ActivityPowerThreshold: 10000, PreferFirstTwoChannels: false,
			},
			DetectPreEcho: true,
		},
		Filter: FilterConfig{
			Refined:                       RefinedFilterConfig{LengthBlocks: 13, LeakageConverged: 0.00005, LeakageDiverged: 0.05, ErrorFloor: 0.001, ErrorCeil: 2, NoiseGate: 20075344},
			Coarse:                        CoarseFilterConfig{LengthBlocks: 13, Rate: 0.7, NoiseGate: 20075344},
			RefinedInitial:                RefinedFilterConfig{LengthBlocks: 12, LeakageConverged: 0.005, LeakageDiverged: 0.5, ErrorFloor: 0.001, ErrorCeil: 2, NoiseGate: 20075344},
			CoarseInitial:                 CoarseFilterConfig{LengthBlocks: 12, Rate: 0.9, NoiseGate: 20075344},
			ConfigChangeDurationBlocks:    250,
			InitialStateSeconds:           2.5,
			CoarseResetHangoverBlocks:     25,
			ConservativeInitialPhase:      false,
			EnableCoarseFilterOutputUsage: true,
			UseLinearFilter:               true,
			HighPassFilterEchoReference:   false,
			ExportLinearAecOutput:         false,
		},
		RenderLevels: RenderLevelsConfig{
			ActiveRenderLimit:            100,
			PoorExcitationRenderLimit:    150,
			PoorExcitationRenderLimitDS8: 20,
			RenderPowerGainDB:            0,
		},
		Erle: ErleConfig{
			Min:                        1,
			MaxL:                       4,
			MaxH:                       1.5,
			OnsetDetection:             true,
			NumSections:                1,
			ClampQualityEstimateToZero: true,
			ClampQualityEstimateToOne:  true,
		},
		EpStrength: EpStrengthConfig{
			DefaultGain:                            1,
			DefaultLen:                             0.83,
			NearendLen:                             0.83,
			EchoCanSaturate:                        true,
			BoundedErl:                             false,
			ErleOnsetCompensationInDominantNearend: false,
			UseConservativeTailFrequencyResponse:   true,
		},
		EchoAudibility: EchoAudibilityConfig{
			LowRenderLimit:                  4 * 64,
			NormalRenderLimit:               64,
			FloorPower:                      2 * 64,
			AudibilityThresholdLF:           10,
			AudibilityThresholdMF:           10,
			AudibilityThresholdHF:           10,
			UseStationarityProperties:       false,
			UseStationarityPropertiesAtInit: false,
		},
		EchoRemovalControl: EchoRemovalControlConfig{
			HasClockDrift:           false,
			LinearAndStableEchoPath: false,
		},
		EchoModel: EchoModelConfig{
			NoiseFloorHold:             50,
			MinNoiseFloorPower:         1638400,
			StationaryGateSlope:        10,
			NoiseGatePower:             27509.42,
			NoiseGateSlope:             0.3,
			RenderPreWindowSize:        1,
			RenderPostWindowSize:       1,
			ModelReverbInNonlinearMode: true,
		},
		ComfortNoise: ComfortNoiseConfig{
			NoiseFloorDbfs: -96.03406,
		},
		Suppressor: SuppressorConfig{
			NearendAverageBlocks: 4,
			NormalTuning: SuppressorTuning{
				MaskLF:         MaskingThresholds{EnrTransparent: .3, EnrSuppress: .4, EmrTransparent: .3},
				MaskHF:         MaskingThresholds{EnrTransparent: .07, EnrSuppress: .1, EmrTransparent: .3},
				MaxIncFactor:   2.0,
				MaxDecFactorLF: 0.25,
			},
			NearendTuning: SuppressorTuning{
				MaskLF:         MaskingThresholds{EnrTransparent: 1.09, EnrSuppress: 1.1, EmrTransparent: .3},
				MaskHF:         MaskingThresholds{EnrTransparent: .1, EnrSuppress: .3, EmrTransparent: .3},
				MaxIncFactor:   2.0,
				MaxDecFactorLF: 0.25,
			},
			LFSmoothingDuringInitialPhase: true,
			LastPermanentLFSmoothingBand:  0,
			LastLFSmoothingBand:           5,
			LastLFBand:                    5,
			FirstHFBand:                   8,
			DominantNearendDetection: DominantNearendDetectionConfig{
				EnrThreshold:             .25,
				EnrExitThreshold:         10,
				SnrThreshold:             30,
				HoldDuration:             50,
				TriggerThreshold:         12,
				UseDuringInitialPhase:    true,
				UseUnboundedEchoSpectrum: true,
			},
			SubbandNearendDetection: SubbandNearendDetectionConfig{
				NearendAverageBlocks: 1,
				Subband1:             SubbandRegion{Low: 1, High: 1},
				Subband2:             SubbandRegion{Low: 1, High: 1},
				NearendThreshold:     1,
				SnrThreshold:         1,
			},
			UseSubbandNearendDetection: false,
			HighBandsSuppression: HighBandsSuppressionConfig{
				EnrThreshold:                   1,
				MaxGainDuringEcho:              1,
				AntiHowlingActivationThreshold: 400,
				AntiHowlingGain:                1,
			},
			FloorFirstIncrease:        0.00001,
			ConservativeHFSuppression: false,
		},
	}
}

func limitInt(v *int, lo, hi int) {
	if *v < lo {
		*v = lo
	} else if *v > hi {
		*v = hi
	}
}

func floorLimitInt(v *int, lo int) {
	if *v < lo {
		*v = lo
	}
}

func limitFloat32(v *float32, lo, hi float32) {
	if !(*v >= lo) { // also catches NaN, matching rtc::SafeClamp
		*v = lo
	} else if *v > hi {
		*v = hi
	}
}

// Validate clamps every field this port defines to the exact ranges
// enforced by EchoCanceller3Config::Validate (api/audio/
// echo_canceller3_config.cc). Cross-field checks (refined vs
// refined_initial length, coarse vs coarse_initial length) are
// applied after the individual range checks, matching the C++
// ordering. Note delay_estimate_smoothing_delay_found is read by
// internal/aec3's MatchedFilter but is, per the oracle source, NOT
// clamped by Validate — that omission is intentional and matches
// upstream.
//
// Validate mutates c in place and never rejects a value outright —
// same as upstream, which silently normalizes an out-of-range field
// rather than failing construction. The aec package's
// CancellerConfig.Tuning validation builds on this: it runs Validate
// on a copy and treats any field Validate had to change as an invalid
// caller input (see aec/validate.go), since a Tuning that Validate
// would silently alter is not the config the caller asked for.
func Validate(c *Config) {
	if c.Delay.DownSamplingFactor != 4 && c.Delay.DownSamplingFactor != 8 {
		c.Delay.DownSamplingFactor = 4
	}
	limitInt(&c.Delay.DefaultDelay, 0, 5000)
	limitInt(&c.Delay.NumFilters, 0, 5000)
	limitInt(&c.Delay.DelayHeadroomSamples, 0, 5000)
	limitInt(&c.Delay.HysteresisLimitBlocks, 0, 5000)
	limitInt(&c.Delay.FixedCaptureDelaySamples, 0, 5000)
	limitFloat32(&c.Delay.DelayEstimateSmoothing, 0, 1)
	limitFloat32(&c.Delay.DelayCandidateDetectionThreshold, 0, 1)
	limitInt(&c.Delay.DelaySelectionThresholds.Initial, 1, 250)
	limitInt(&c.Delay.DelaySelectionThresholds.Converged, 1, 250)

	floorLimitInt(&c.Filter.Refined.LengthBlocks, 1)
	limitFloat32(&c.Filter.Refined.LeakageConverged, 0, 1000)
	limitFloat32(&c.Filter.Refined.LeakageDiverged, 0, 1000)
	limitFloat32(&c.Filter.Refined.ErrorFloor, 0, 1000)
	limitFloat32(&c.Filter.Refined.ErrorCeil, 0, 100000000)
	limitFloat32(&c.Filter.Refined.NoiseGate, 0, 100000000)

	floorLimitInt(&c.Filter.RefinedInitial.LengthBlocks, 1)
	limitFloat32(&c.Filter.RefinedInitial.LeakageConverged, 0, 1000)
	limitFloat32(&c.Filter.RefinedInitial.LeakageDiverged, 0, 1000)
	limitFloat32(&c.Filter.RefinedInitial.ErrorFloor, 0, 1000)
	limitFloat32(&c.Filter.RefinedInitial.ErrorCeil, 0, 100000000)
	limitFloat32(&c.Filter.RefinedInitial.NoiseGate, 0, 100000000)
	if c.Filter.Refined.LengthBlocks < c.Filter.RefinedInitial.LengthBlocks {
		c.Filter.RefinedInitial.LengthBlocks = c.Filter.Refined.LengthBlocks
	}

	floorLimitInt(&c.Filter.Coarse.LengthBlocks, 1)
	limitFloat32(&c.Filter.Coarse.Rate, 0, 1)
	limitFloat32(&c.Filter.Coarse.NoiseGate, 0, 100000000)

	floorLimitInt(&c.Filter.CoarseInitial.LengthBlocks, 1)
	limitFloat32(&c.Filter.CoarseInitial.Rate, 0, 1)
	limitFloat32(&c.Filter.CoarseInitial.NoiseGate, 0, 100000000)
	if c.Filter.Coarse.LengthBlocks < c.Filter.CoarseInitial.LengthBlocks {
		c.Filter.CoarseInitial.LengthBlocks = c.Filter.Coarse.LengthBlocks
	}

	limitInt(&c.Filter.ConfigChangeDurationBlocks, 0, 100000)
	limitFloat32(&c.Filter.InitialStateSeconds, 0, 100)
	limitInt(&c.Filter.CoarseResetHangoverBlocks, 0, 250000)

	limitFloat32(&c.Erle.Min, 1, 100000)
	limitFloat32(&c.Erle.MaxL, 1, 100000)
	limitFloat32(&c.Erle.MaxH, 1, 100000)
	if c.Erle.Min > c.Erle.MaxL || c.Erle.Min > c.Erle.MaxH {
		if c.Erle.MaxL < c.Erle.MaxH {
			c.Erle.Min = c.Erle.MaxL
		} else {
			c.Erle.Min = c.Erle.MaxH
		}
	}
	limitInt(&c.Erle.NumSections, 1, c.Filter.Refined.LengthBlocks)

	limitFloat32(&c.EpStrength.DefaultGain, 0, 1000000)
	limitFloat32(&c.EpStrength.DefaultLen, -1, 1)
	limitFloat32(&c.EpStrength.NearendLen, -1, 1)

	limitFloat32(&c.EchoAudibility.LowRenderLimit, 0, 32768*32768)
	limitFloat32(&c.EchoAudibility.NormalRenderLimit, 0, 32768*32768)
	limitFloat32(&c.EchoAudibility.FloorPower, 0, 32768*32768)
	limitFloat32(&c.EchoAudibility.AudibilityThresholdLF, 0, 32768*32768)
	limitFloat32(&c.EchoAudibility.AudibilityThresholdMF, 0, 32768*32768)
	limitFloat32(&c.EchoAudibility.AudibilityThresholdHF, 0, 32768*32768)

	limitFloat32(&c.RenderLevels.ActiveRenderLimit, 0, 32768*32768)
	limitFloat32(&c.RenderLevels.PoorExcitationRenderLimit, 0, 32768*32768)
	limitFloat32(&c.RenderLevels.PoorExcitationRenderLimitDS8, 0, 32768*32768)

	limitInt(&c.EchoModel.NoiseFloorHold, 0, 1000)
	limitFloat32(&c.EchoModel.MinNoiseFloorPower, 0, 2000000)
	limitFloat32(&c.EchoModel.StationaryGateSlope, 0, 1000000)
	limitFloat32(&c.EchoModel.NoiseGatePower, 0, 1000000)
	limitFloat32(&c.EchoModel.NoiseGateSlope, 0, 1000000)
	limitInt(&c.EchoModel.RenderPreWindowSize, 0, 100)
	limitInt(&c.EchoModel.RenderPostWindowSize, 0, 100)

	limitFloat32(&c.ComfortNoise.NoiseFloorDbfs, -200, 0)

	limitInt(&c.Suppressor.NearendAverageBlocks, 1, 5000)

	limitFloat32(&c.Suppressor.NormalTuning.MaskLF.EnrTransparent, 0, 100)
	limitFloat32(&c.Suppressor.NormalTuning.MaskLF.EnrSuppress, 0, 100)
	limitFloat32(&c.Suppressor.NormalTuning.MaskLF.EmrTransparent, 0, 100)
	limitFloat32(&c.Suppressor.NormalTuning.MaskHF.EnrTransparent, 0, 100)
	limitFloat32(&c.Suppressor.NormalTuning.MaskHF.EnrSuppress, 0, 100)
	limitFloat32(&c.Suppressor.NormalTuning.MaskHF.EmrTransparent, 0, 100)
	limitFloat32(&c.Suppressor.NormalTuning.MaxIncFactor, 0, 100)
	limitFloat32(&c.Suppressor.NormalTuning.MaxDecFactorLF, 0, 100)

	limitFloat32(&c.Suppressor.NearendTuning.MaskLF.EnrTransparent, 0, 100)
	limitFloat32(&c.Suppressor.NearendTuning.MaskLF.EnrSuppress, 0, 100)
	limitFloat32(&c.Suppressor.NearendTuning.MaskLF.EmrTransparent, 0, 100)
	limitFloat32(&c.Suppressor.NearendTuning.MaskHF.EnrTransparent, 0, 100)
	limitFloat32(&c.Suppressor.NearendTuning.MaskHF.EnrSuppress, 0, 100)
	limitFloat32(&c.Suppressor.NearendTuning.MaskHF.EmrTransparent, 0, 100)
	limitFloat32(&c.Suppressor.NearendTuning.MaxIncFactor, 0, 100)
	limitFloat32(&c.Suppressor.NearendTuning.MaxDecFactorLF, 0, 100)

	limitInt(&c.Suppressor.LastPermanentLFSmoothingBand, 0, 64)
	limitInt(&c.Suppressor.LastLFSmoothingBand, 0, 64)
	limitInt(&c.Suppressor.LastLFBand, 0, 63)
	limitInt(&c.Suppressor.FirstHFBand, c.Suppressor.LastLFBand+1, 64)

	limitFloat32(&c.Suppressor.DominantNearendDetection.EnrThreshold, 0, 1000000)
	limitFloat32(&c.Suppressor.DominantNearendDetection.SnrThreshold, 0, 1000000)
	limitInt(&c.Suppressor.DominantNearendDetection.HoldDuration, 0, 10000)
	limitInt(&c.Suppressor.DominantNearendDetection.TriggerThreshold, 0, 10000)

	limitInt(&c.Suppressor.SubbandNearendDetection.NearendAverageBlocks, 1, 1024)
	limitInt(&c.Suppressor.SubbandNearendDetection.Subband1.Low, 0, 65)
	limitInt(&c.Suppressor.SubbandNearendDetection.Subband1.High, c.Suppressor.SubbandNearendDetection.Subband1.Low, 65)
	limitInt(&c.Suppressor.SubbandNearendDetection.Subband2.Low, 0, 65)
	limitInt(&c.Suppressor.SubbandNearendDetection.Subband2.High, c.Suppressor.SubbandNearendDetection.Subband2.Low, 65)
	limitFloat32(&c.Suppressor.SubbandNearendDetection.NearendThreshold, 0, 1e24)
	limitFloat32(&c.Suppressor.SubbandNearendDetection.SnrThreshold, 0, 1e24)

	limitFloat32(&c.Suppressor.HighBandsSuppression.EnrThreshold, 0, 1000000)
	limitFloat32(&c.Suppressor.HighBandsSuppression.MaxGainDuringEcho, 0, 1)
	limitFloat32(&c.Suppressor.HighBandsSuppression.AntiHowlingActivationThreshold, 0, 32768*32768)
	limitFloat32(&c.Suppressor.HighBandsSuppression.AntiHowlingGain, 0, 1)

	limitFloat32(&c.Suppressor.FloorFirstIncrease, 0, 1000000)
}
