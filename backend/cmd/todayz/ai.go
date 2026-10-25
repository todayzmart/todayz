package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/settings"

	// Eval suites register themselves; a feature package adds its import here.
	_ "github.com/todayz/backend/internal/ai/bannercopy/bannereval"
	_ "github.com/todayz/backend/internal/ai/billscan/billeval"
	_ "github.com/todayz/backend/internal/ai/catalogue/catalogueeval"
	_ "github.com/todayz/backend/internal/ai/eval/selftest"
	_ "github.com/todayz/backend/internal/ai/imagecheck/imageeval"
	_ "github.com/todayz/backend/internal/ai/insighttext/insighteval"
	_ "github.com/todayz/backend/internal/ai/replydraft/replyeval"
	_ "github.com/todayz/backend/internal/ai/riskexplain/riskeval"
	_ "github.com/todayz/backend/internal/ai/shelfscan/shelfeval"
	_ "github.com/todayz/backend/internal/ai/triage/triageeval"
	_ "github.com/todayz/backend/internal/coach/coacheval"
	_ "github.com/todayz/backend/internal/voice/voiceeval"
)

// evalCallTimeout bounds one live model call in an eval.
const evalCallTimeout = 60 * time.Second //settings:ok an operator guard for the eval CLI, not a product number

// runAI is `todayz ai eval <feature|all> [--live]` (W3-10, 11 §1 rule 8).
//
// Recorded (the default, make check): every case is answered by its recorded reply through a
// loopback server, so it needs no key, no database and no network. --live (make ai-eval)
// calls the real model with the user's own ANTHROPIC_API_KEY from .env.local and reads the
// models from platform_settings (DATABASE_URL); without the key it says which names are
// missing and stops. It exits non-zero when a suite scores under its pass bar.
func runAI(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	if len(args) == 0 || args[0] != "eval" {
		return usageError{msg: "ai takes: eval <feature|all> [--live]"}
	}
	fs := flag.NewFlagSet("ai eval", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	live := fs.Bool("live", false, "call the live model (make ai-eval)")
	if err := fs.Parse(reorder(args[1:])); err != nil || fs.NArg() != 1 {
		return usageError{msg: "ai eval takes <feature|all> [--live]"}
	}
	want := fs.Arg(0)
	features := eval.Features()
	if want != "all" {
		if _, ok := eval.Get(want); !ok {
			return usageError{msg: fmt.Sprintf("no eval suite %q (have: %s)", want, strings.Join(features, ", "))}
		}
		features = []string{want}
	}

	var env eval.Env
	var newLLM func(cases []eval.Case, feature string) (client.LLM, func())
	mode := "recorded"
	if *live {
		mode = "live"
		cfg, _, err := cliBoot("ai-eval", getenv, out)
		if err != nil {
			return err
		}
		var missing []string
		if cfg.Providers.LLMAPIURL == "" {
			missing = append(missing, "LLM_API_URL")
		}
		if !cfg.Providers.AnthropicAPIKey.IsSet() {
			missing = append(missing, "ANTHROPIC_API_KEY")
		}
		if !cfg.DatabaseURL.IsSet() {
			missing = append(missing, "DATABASE_URL")
		}
		if len(missing) > 0 {
			return fmt.Errorf("live evals need %s in .env.local (your own key, D-17)", strings.Join(missing, ", "))
		}
		pool, err := db.Open(ctx, cfg.DatabaseURL.Reveal(), db.Options{AppName: "todayz-ai-eval", MaxConns: 2})
		if err != nil {
			return err
		}
		defer pool.Close()
		cache := settings.NewCache(settings.PGLoader(pool), settings.Options{})
		models := map[settings.Key]string{}
		for _, k := range []settings.Key{settings.AiModelFast, settings.AiModelSmart} {
			if models[k], err = cache.String(ctx, k); err != nil {
				return err
			}
		}
		env.Model = func(k settings.Key) string { return models[k] }
		hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: cfg.NoEgress, Timeout: evalCallTimeout})
		llm := client.NewMetered(client.NewAnthropic(cfg.Providers.LLMAPIURL, cfg.Providers.AnthropicAPIKey, hc), clock.Real{})
		newLLM = func([]eval.Case, string) (client.LLM, func()) { return llm, func() {} }
	} else {
		env.Model = func(settings.Key) string { return "recorded" }
		hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true})
		newLLM = func(cases []eval.Case, feature string) (client.LLM, func()) {
			rp := eval.NewReplay(feature, cases)
			return client.NewMetered(client.NewAnthropic(rp.URL(), config.Secret{}, hc), clock.Real{}), rp.Close
		}
	}

	var below []string
	for _, f := range features {
		s, _ := eval.Get(f)
		cases, err := eval.LoadCases(s, !*live)
		if err != nil {
			return err
		}
		llm, done := newLLM(cases, f)
		env.LLM = llm
		rep, err := eval.Run(ctx, s, cases, env, mode)
		done()
		if err != nil {
			return err
		}
		verdict := "ok"
		if !rep.OK {
			verdict = "BELOW BAR"
			below = append(below, f)
		}
		_, _ = fmt.Fprintf(out, "%-18s %-8s %d/%d score %.2f bar %.2f %s  cost %d paise  p95 %s\n",
			f, mode, rep.Passed, rep.Cases, rep.Score, rep.PassBar, verdict, rep.CostPaise, rep.LatencyP95.Round(time.Millisecond))
		for _, fl := range rep.Failures {
			_, _ = fmt.Fprintf(out, "  fail %s: %s\n", fl.Case, fl.Note)
		}
	}
	if len(below) > 0 {
		return fmt.Errorf("%w: %s", eval.ErrBelowBar, strings.Join(below, ", "))
	}
	if len(features) == 0 {
		return errors.New("no eval suites registered")
	}
	return nil
}

// reorder puts flags before the positional argument, so `ai eval all --live` parses.
func reorder(args []string) []string {
	var flags, rest []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			rest = append(rest, a)
		}
	}
	return append(flags, rest...)
}
