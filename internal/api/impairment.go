package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/sim/impair"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type impairmentFields struct {
	Unplugged bool    `json:"unplugged" required:"false" doc:"Dark on every protocol: connects time out, datagrams and pings go unanswered"`
	LatencyMs int     `json:"latency_ms" required:"false" minimum:"0" maximum:"10000" doc:"Delay added to traffic toward the device, in milliseconds"`
	JitterMs  int     `json:"jitter_ms" required:"false" minimum:"0" doc:"Variation on the delay, in milliseconds; at most the latency"`
	LossPct   float64 `json:"loss_pct" required:"false" minimum:"0" maximum:"100" doc:"Share of packets toward the device dropped, in percent"`
	RateKbit  int     `json:"rate_kbit" required:"false" minimum:"0" doc:"Bandwidth limit toward the device, in kilobits per second; 0 is unlimited"`
}

type impairmentPathInput struct {
	Name string `path:"name" doc:"The simulated component's name, or a dotted address"`
}

type setImpairmentInput struct {
	Name string `path:"name" doc:"The simulated component's name, or a dotted address"`
	Body impairmentFields
}

type impairmentOutput struct {
	Body impairmentFields
}

func registerImpairmentRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-component-impairment",
		Method:      http.MethodGet,
		Path:        "/components/{name}/impairment",
		Summary:     "Get what is broken on a simulated device",
		Description: "The impairment an operator set on a simulated component (unplugged, or a degraded link), applied by the simulator beside its node when that simulator runs with impairments and NET_ADMIN. A healthy device reads all zero. Gated by simulation:read; the component resolves within the caller's component read scope.",
	}, "simulation", "read"), func(ctx context.Context, in *impairmentPathInput) (*impairmentOutput, error) {
		id, err := gw.ResolveActionTarget(ctx, "component", in.Name, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "simulation", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		imp, err := gw.GetSimImpairment(ctx, id)
		if err != nil {
			return nil, huma.Error500InternalServerError("get impairment")
		}
		return &impairmentOutput{Body: impairmentFields(imp)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-component-impairment",
		Method:      http.MethodPut,
		Path:        "/components/{name}/impairment",
		Summary:     "Break or repair a simulated device",
		Description: "Sets what is broken on a simulated component: unplugged, or a link with added latency, jitter, loss or a rate limit. An all-zero body repairs it. Audited. The simulator beside the component's node applies it on its next pull. Gated by simulation:update, whose scope is resolved on the component tier; a component outside the caller's component:read is a non-disclosing 404, and one it can read but not impair is a 403. Jitter above latency, or a value outside its range, is a 422.",
	}, "simulation", "update"), func(ctx context.Context, in *setImpairmentInput) (*impairmentOutput, error) {
		id, err := gw.ResolveActionTarget(ctx, "component", in.Name, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "simulation", "update"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		imp := impair.Impairment(in.Body)
		if err := gw.SetSimImpairment(ctx, actorID(ctx), id, imp); err != nil {
			if errors.Is(err, storage.ErrSimImpairmentInvalid) {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return nil, huma.Error500InternalServerError("set impairment")
		}
		return &impairmentOutput{Body: in.Body}, nil
	})
}
