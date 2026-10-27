package ops

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/smithy-go"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	api "github.com/todayz/backend/gen/api/admin-system"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/ids"
)

var t0 = time.Date(2026, 10, 25, 6, 32, 10, 0, time.UTC)

// fakeAWS counts calls per panel and answers from its fields.
type fakeAWS struct {
	envCalls, albCalls, rdsCalls, alarmCalls atomic.Int32

	envs      []Environment
	envErr    error
	alb       ALBStats
	albErr    error
	albWindow [2]time.Time
	albLBs    []string
	rds       []api.AdminSystemRdsInstance
	rdsErr    error
	alarms    []api.AdminSystemAlarm
	alarmErr  error
	mu        sync.Mutex
}

func (f *fakeAWS) Environments(context.Context) ([]Environment, error) {
	f.envCalls.Add(1)
	return f.envs, f.envErr
}

func (f *fakeAWS) ALB(_ context.Context, lbs []string, start, end time.Time) (ALBStats, error) {
	f.albCalls.Add(1)
	f.mu.Lock()
	f.albLBs, f.albWindow = lbs, [2]time.Time{start, end}
	f.mu.Unlock()
	return f.alb, f.albErr
}

func (f *fakeAWS) RDS(context.Context, time.Time) ([]api.AdminSystemRdsInstance, error) {
	f.rdsCalls.Add(1)
	return append([]api.AdminSystemRdsInstance(nil), f.rds...), f.rdsErr
}

func (f *fakeAWS) Alarms(context.Context) ([]api.AdminSystemAlarm, error) {
	f.alarmCalls.Add(1)
	return append([]api.AdminSystemAlarm(nil), f.alarms...), f.alarmErr
}

func (f *fakeAWS) calls() int32 {
	return f.envCalls.Load() + f.albCalls.Load() + f.rdsCalls.Load() + f.alarmCalls.Load()
}

func ptr[T any](v T) *T { return &v }

func healthyAWS() *fakeAWS {
	return &fakeAWS{
		envs: []Environment{{
			AdminSystemEnvironment: api.AdminSystemEnvironment{Name: "todayz-prod-api", Health: api.Green, Status: "Ready",
				VersionLabel: ptr("v42"), InstanceCount: ptr(2), Deploys: []api.AdminSystemDeploy{}},
			LoadBalancers: []string{"app/awseb-x/1"},
		}},
		alb: ALBStats{Requests: ptr(600.0), HTTP5xx: ptr(3.0), P95TargetSec: ptr(0.1234)},
		rds: []api.AdminSystemRdsInstance{{Name: "todayz-prod", Status: "available", MultiAz: false, CpuPercent: ptr(12.5)}},
		alarms: []api.AdminSystemAlarm{
			{Name: "b-ok", State: api.AdminSystemAlarmStateOk},
			{Name: "z-alarm", State: api.AdminSystemAlarmStateAlarm},
			{Name: "a-ok", State: api.AdminSystemAlarmStateOk},
			{Name: "m-insufficient", State: api.AdminSystemAlarmStateInsufficientData},
		},
	}
}

// SC-OPS-01, SC-OPS-09 (U): off AWS every AWS panel is not_configured, no
// adapter exists, and Multi-AZ is "off" as a fact.
func TestNotConfiguredOffAWS(t *testing.T) {
	s := New(Deps{Env: api.Local, Clock: clock.NewFake(t0)})
	out := s.System(context.Background())
	require.False(t, out.AwsConfigured)
	require.Equal(t, api.Local, out.Env)
	for name, st := range map[string]api.AdminSystemPanelStatus{
		"beanstalk": out.Beanstalk.Status, "alb": out.Alb.Status, "rds": out.Rds.Status, "alarms": out.Alarms.Status,
	} {
		require.Equal(t, api.AdminSystemPanelStatusNotConfigured, st, name)
	}
	require.Nil(t, out.Alarms.ErrorClass)
	require.Empty(t, out.Beanstalk.Environments)
	require.NotNil(t, out.Beanstalk.Environments, "an empty list, never null")
	require.Equal(t, 5, out.Alb.WindowMinutes)
	require.Equal(t, ptr(false), out.Database.MultiAz)
	require.Equal(t, t0, out.ServerNow)
}

// SC-OPS-09 (U): with AWS keys in the process environment, local and test
// still build no AWS client; staging and prod do.
func TestNoAWSClientOutsideStagingAndProd(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAFAKEFAKEFAKEFAKE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-for-the-test")
	t.Setenv("AWS_PROFILE", "default")
	for _, env := range []config.Env{config.EnvLocal, config.EnvTest} {
		cfg := &config.Config{Env: env, AWSRegion: "ap-south-1", NoEgress: true}
		a, err := NewAWS(cfg)
		require.ErrorIs(t, err, ErrNotDeployed, env)
		require.Nil(t, a, env)
		s := NewFromConfig(cfg, nil, clock.NewFake(t0))
		require.Nil(t, s.d.AWS, env)
		require.NoError(t, s.d.AWSErr, env)
		out := s.System(context.Background())
		require.False(t, out.AwsConfigured, env)
		require.Equal(t, api.AdminSystemPanelStatusNotConfigured, out.Rds.Status, env)
		require.Equal(t, api.AdminSystemEnv(env), out.Env)
	}
	for _, env := range []config.Env{config.EnvStaging, config.EnvProd} {
		s := NewFromConfig(&config.Config{Env: env, AWSRegion: "ap-south-1", NoEgress: true}, nil, clock.NewFake(t0))
		require.NotNil(t, s.d.AWS, env)
	}
	// Deployed without a region: every AWS panel says so, nothing is called.
	s := NewFromConfig(&config.Config{Env: config.EnvProd, NoEgress: true}, nil, clock.NewFake(t0))
	require.ErrorIs(t, s.d.AWSErr, ErrNoRegion)
	out := s.System(context.Background())
	require.True(t, out.AwsConfigured)
	require.Equal(t, api.AdminSystemPanelStatusError, out.Alarms.Status)
	require.Equal(t, ptr("Error"), out.Alarms.ErrorClass)
}

// SC-OPS-09 (U): the deployed client gets credentials from the instance role
// over IMDSv2 on the pinned address, never from AWS_* variables: the first
// request is the IMDSv2 token PUT, and with no role nothing reaches AWS.
func TestDeployedClientUsesTheInstanceRoleOnly(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAFAKEFAKEFAKEFAKE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-for-the-test")
	t.Setenv("AWS_EC2_METADATA_SERVICE_ENDPOINT", "http://127.0.0.1:1")
	var mu sync.Mutex
	var seen []string
	wrap := func(kind string, _ http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			mu.Lock()
			seen = append(seen, kind+" "+r.Method+" "+r.URL.Host+r.URL.Path)
			mu.Unlock()
			return nil, errors.New("no network in tests")
		})
	}
	c, err := newAWS(&config.Config{Env: config.EnvProd, AWSRegion: "ap-south-1"}, wrap)
	require.NoError(t, err)
	_, err = c.Alarms(context.Background())
	require.Error(t, err)
	require.Equal(t, "Credentials", ErrorClass(err))
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, seen)
	require.Equal(t, "imds PUT 169.254.169.254/latest/api/token", seen[0])
	for _, s := range seen {
		require.Contains(t, s, "imds ", "no AWS API request without role credentials: %v", seen)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// SC-OPS-04 (U): two calls within 60 s make one fetch; 61 s later a second.
func TestAWSCache(t *testing.T) {
	clk := clock.NewFake(t0)
	f := healthyAWS()
	s := New(Deps{Env: api.Prod, Clock: clk, AWS: f})
	first := s.System(context.Background())
	require.Equal(t, int32(4), f.calls(), "one fetch reads every panel once")
	clk.Advance(59 * time.Second)
	second := s.System(context.Background())
	require.Equal(t, int32(4), f.calls(), "cached within 60 s")
	require.Equal(t, first.Alarms.FetchedAt, second.Alarms.FetchedAt, "the page shows the data's age")
	require.Equal(t, t0.Add(59*time.Second), second.ServerNow)
	clk.Advance(2 * time.Second)
	third := s.System(context.Background())
	require.Equal(t, int32(8), f.calls(), "61 s later: a second fetch")
	require.Equal(t, t0.Add(61*time.Second), third.Alarms.FetchedAt)
}

// Concurrent requests during a fetch share it.
func TestAWSCacheIsOneFetchAtATime(t *testing.T) {
	f := healthyAWS()
	s := New(Deps{Env: api.Prod, Clock: clock.NewFake(t0), AWS: f})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); s.System(context.Background()) }()
	}
	wg.Wait()
	require.Equal(t, int32(1), f.envCalls.Load())
}

// A failed fetch is cached too, and a cancelled caller does not cancel it.
func TestAWSErrorsAreCachedAndDetached(t *testing.T) {
	clk := clock.NewFake(t0)
	f := healthyAWS()
	f.alarmErr = &smithy.GenericAPIError{Code: "Throttling", Message: "Rate exceeded for arn:aws:iam::123456789012:role/x"}
	s := New(Deps{Env: api.Prod, Clock: clk, AWS: f})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := s.System(ctx)
	require.Equal(t, api.AdminSystemPanelStatusOk, out.Beanstalk.Status, "the fetch ran on a detached context")
	require.Equal(t, ptr("Throttling"), out.Alarms.ErrorClass)
	s.System(context.Background())
	require.Equal(t, int32(1), f.alarmCalls.Load())
}

// SC-OPS-08 (U): CloudWatch access denied → the alarms panel says error with
// the class and nothing else; the other panels still render.
func TestOnePanelFailing(t *testing.T) {
	f := healthyAWS()
	f.alarmErr = &smithy.GenericAPIError{Code: "AccessDenied", Message: "User: arn:aws:sts::123456789012:assumed-role/x is not authorized"}
	s := New(Deps{Env: api.Prod, Clock: clock.NewFake(t0), AWS: f})
	out := s.System(context.Background())
	require.True(t, out.AwsConfigured)
	require.Equal(t, api.AdminSystemPanelStatusError, out.Alarms.Status)
	require.Equal(t, ptr("AccessDenied"), out.Alarms.ErrorClass)
	require.Empty(t, out.Alarms.Items)
	require.Equal(t, api.AdminSystemPanelStatusOk, out.Beanstalk.Status)
	require.Equal(t, api.AdminSystemPanelStatusOk, out.Alb.Status)
	require.Equal(t, api.AdminSystemPanelStatusOk, out.Rds.Status)
	require.Len(t, out.Beanstalk.Environments, 1)
	require.Equal(t, ptr(false), out.Database.MultiAz, "one RDS instance: its Multi-AZ answer")
}

// Beanstalk failing takes the ALB panel with it (it has no load balancers to
// ask about); RDS and alarms stand.
func TestBeanstalkFailingTakesTheALB(t *testing.T) {
	f := healthyAWS()
	f.envErr = context.DeadlineExceeded
	s := New(Deps{Env: api.Prod, Clock: clock.NewFake(t0), AWS: f})
	out := s.System(context.Background())
	require.Equal(t, ptr("Timeout"), out.Beanstalk.ErrorClass)
	require.Equal(t, api.AdminSystemPanelStatusError, out.Alb.Status)
	require.Equal(t, ptr("Timeout"), out.Alb.ErrorClass)
	require.Equal(t, int32(0), f.albCalls.Load())
	require.Equal(t, api.AdminSystemPanelStatusOk, out.Rds.Status)
	require.Len(t, out.Alarms.Items, 4)
}

func TestPanelFigures(t *testing.T) {
	f := healthyAWS()
	f.rds = append(f.rds, api.AdminSystemRdsInstance{Name: "a-replica", MultiAz: true})
	s := New(Deps{Env: api.Prod, Clock: clock.NewFake(t0), AWS: f})
	out := s.System(context.Background())
	// The last complete 5-minute period: 06:25–06:30 for 06:32:10.
	require.Equal(t, [2]time.Time{t0.Truncate(5 * time.Minute).Add(-5 * time.Minute), t0.Truncate(5 * time.Minute)}, f.albWindow)
	require.Equal(t, []string{"app/awseb-x/1"}, f.albLBs)
	require.Equal(t, ptr(float64(120)), out.Alb.RequestsPerMinute)
	require.Equal(t, ptr(50), out.Alb.Http5xxRateBp, "3 of 600 = 0.5%")
	require.Equal(t, ptr(123), out.Alb.P95Ms)
	names := make([]string, len(out.Alarms.Items))
	for i, a := range out.Alarms.Items {
		names[i] = a.Name
	}
	require.Equal(t, []string{"z-alarm", "m-insufficient", "a-ok", "b-ok"}, names)
	require.Equal(t, "a-replica", out.Rds.Instances[0].Name)
	require.Nil(t, out.Database.MultiAz, "two instances: unknown, not guessed")

	p := albPanel(t0, ALBStats{Requests: ptr(0.0)}, nil)
	require.Equal(t, ptr(0.0), p.RequestsPerMinute)
	require.Nil(t, p.Http5xxRateBp, "no requests: no rate")
	require.Nil(t, albPanel(t0, ALBStats{}, nil).RequestsPerMinute)
}

func TestErrorClass(t *testing.T) {
	for err, want := range map[error]string{
		&smithy.GenericAPIError{Code: "AccessDenied"}:               "AccessDenied",
		&smithy.GenericAPIError{Code: "Bad code: arn:aws:x"}:        "Error",
		&credentialsError{cause: errors.New("role arn:aws:iam::1")}: "Credentials",
		context.DeadlineExceeded:                                    "Timeout",
		errors.New("dial tcp 10.0.0.1:443: refused"):                "Error",
	} {
		require.Equal(t, want, ErrorClass(err), err.Error())
	}
	require.NotContains(t, (&credentialsError{cause: errors.New("arn:aws:iam::1")}).Error(), "arn")
}

func TestBeanstalkMapping(t *testing.T) {
	require.Equal(t, api.Green, health("Green"))
	require.Equal(t, api.Grey, health("Grey"))
	require.Equal(t, api.Unknown, health(""))
	for msg, want := range map[string]api.AdminSystemDeployOutcome{
		"Environment update completed successfully.":                      api.Succeeded,
		"Failed to deploy application.":                                   api.Failed,
		"Update environment operation is complete, but with errors. For…": api.Failed,
	} {
		got, ok := deployOutcome(msg)
		require.True(t, ok, msg)
		require.Equal(t, want, got, msg)
	}
	_, ok := deployOutcome("Added instance [i-0abc] to your environment.")
	require.False(t, ok)
	require.Equal(t, "app/awseb-AWSEB-1/50dc6c495c0c9188",
		albDimension("arn:aws:elasticloadbalancing:ap-south-1:123456789012:loadbalancer/app/awseb-AWSEB-1/50dc6c495c0c9188"))
	require.Equal(t, "app/x/1", albDimension("app/x/1"))
	require.Empty(t, albDimension("awseb-e-classic"), "a classic ELB has no ALB metrics")
}

// SC-OPS-03 (U): admin.system.view is admin and super_admin; every other
// admin role is 403, and no principal is 401.
func TestRoleGate(t *testing.T) {
	e, err := authz.New(policy.All(), authz.Options{Clock: clock.NewFake(t0)})
	require.NoError(t, err)
	r := chi.NewRouter()
	Register(r, New(Deps{Env: api.Local, Clock: clock.NewFake(t0)}), e)
	call := func(p *authn.Principal) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/system", nil)
		if p != nil {
			req = req.WithContext(authn.WithPrincipal(req.Context(), *p))
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	for role, want := range map[string]int{
		"admin": 200, "super_admin": 200,
		"support": 403, "catalogue": 403, "analyst": 403, "finance": 403, "field_agent": 403,
	} {
		p := authn.Principal{UserID: ids.New(t0), SessionID: ids.New(t0), App: authn.AppAdmin, AMR: authn.AMROTP, Roles: []string{role}}
		require.Equal(t, want, call(&p), role)
	}
	require.Equal(t, 401, call(nil))
}

// Every admin-system operation has an action (a missing one fails closed
// with 500), and it is admin.system.view.
func TestOpsCoverEveryOperation(t *testing.T) {
	it := reflect.TypeOf((*api.StrictServerInterface)(nil)).Elem()
	require.Equal(t, it.NumMethod(), len(SystemOps))
	for i := 0; i < it.NumMethod(); i++ {
		require.Equal(t, policy.AdminSystemView, SystemOps[it.Method(i).Name], it.Method(i).Name)
	}
}
