package hostinfo

import (
	"context"

	"github.com/go-kit/log"
	"github.com/grafana/tempo/modules/generator/registry"
	"github.com/grafana/tempo/pkg/tempopb"
	v1 "github.com/grafana/tempo/pkg/tempopb/trace/v1"
)

const (
	Name = "host-info"

	hostInfoMetric     = "traces_host_info"
	hostIdentifierAttr = "grafana_host_id"
)

type Processor struct {
	Cfg    Config
	logger log.Logger

	gauge      registry.Gauge
	registry   registry.Registry
	metricName string
	labels     []string
}

func (p *Processor) Name() string {
	return Name
}

func (p *Processor) findHostIdentifier(resourceSpans *v1.ResourceSpans) string {
	attrs := resourceSpans.GetResource().GetAttributes()
	for _, idAttr := range p.Cfg.HostIdentifiers {
		for _, attr := range attrs {
			if attr.GetKey() == idAttr {
				if val := attr.GetValue(); val != nil {
					if strVal := val.GetStringValue(); strVal != "" {
						return strVal
					}
				}
			}
		}
	}
	return ""
}

func (p *Processor) PushSpans(_ context.Context, req *tempopb.PushSpansRequest) {
	values := make([]string, 1)
	for i := range req.Batches {
		resourceSpans := req.Batches[i]
		if hostID := p.findHostIdentifier(resourceSpans); hostID != "" {
			values[0] = hostID
			labelValues := p.registry.NewLabelValueCombo(
				p.labels,
				values,
			)
			p.gauge.Set(labelValues, 1)
		}
	}
}

func (p *Processor) Shutdown(_ context.Context) {}

func New(cfg Config, reg registry.Registry, logger log.Logger) (*Processor, error) {
	labels := make([]string, 1)
	labels[0] = hostIdentifierAttr
	p := &Processor{
		Cfg:        cfg,
		logger:     logger,
		registry:   reg,
		metricName: cfg.MetricName,
		gauge:      reg.NewGauge(cfg.MetricName),
		labels:     labels,
	}
	return p, nil
}
