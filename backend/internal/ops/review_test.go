package ops

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk/types"
	"github.com/aws/smithy-go"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	api "github.com/todayz/backend/gen/api/admin-system"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/ids"
)

// slowAWS is a fakeAWS whose Beanstalk read takes a while, as a slow AWS does.
type slowAWS struct {
	*fakeAWS
	delay time.Duration
}

func (s slowAWS) Environments(ctx context.Context) ([]Environment, error) {
	time.Sleep(s.delay)
	return s.fakeAWS.Environments(ctx)
}

// With a real clock, callers that queued behind a fetch take its result: one
// fetch for all of them, not one per caller whose clock reading came first.
func TestAWSCacheOneFetchWithARealClock(t *testing.T) {
	f := healthyAWS()
	s := New(Deps{Env: api.Prod, Clock: clock.Real{}, AWS: slowAWS{f, 50 * time.Millisecond}})
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out := s.System(context.Background())
			require.False(t, out.ServerNow.Before(out.Beanstalk.FetchedAt), "no fetchedAt after serverNow")
		}()
	}
	close(start)
	wg.Wait()
	require.Equal(t, int32(1), f.envCalls.Load())
	require.Equal(t, int32(1), f.alarmCalls.Load())
}

// The database and River panels share the fix: concurrent callers with a real
// clock read the database once.
func TestDBCacheOneReadWithARealClock(t *testing.T) {
	s := New(Deps{Env: api.Local, Clock: clock.Real{}})
	var wg sync.WaitGroup
	snaps := make([]*dbSnapshot, 16)
	for i := range snaps {
		wg.Add(1)
		go func() { defer wg.Done(); snaps[i] = s.dbPanels(context.Background()) }()
	}
	wg.Wait()
	for _, x := range snaps {
		require.Same(t, snaps[0], x, "one read, shared")
	}
}

// fakeCW answers DescribeAlarms like CloudWatch: filtered by StateValue, in
// name order, MaxRecords per page.
type fakeCW struct {
	alarms []cwtypes.MetricAlarm
	asked  []cwtypes.StateValue
}

func (f *fakeCW) DescribeAlarms(_ context.Context, in *cloudwatch.DescribeAlarmsInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.DescribeAlarmsOutput, error) {
	if in.NextToken == nil {
		f.asked = append(f.asked, in.StateValue)
	}
	var match []cwtypes.MetricAlarm
	for _, a := range f.alarms {
		if in.StateValue == "" || a.StateValue == in.StateValue {
			match = append(match, a)
		}
	}
	from := 0
	if in.NextToken != nil {
		from, _ = strconv.Atoi(*in.NextToken)
	}
	to := min(from+int(aws.ToInt32(in.MaxRecords)), len(match))
	out := &cloudwatch.DescribeAlarmsOutput{MetricAlarms: match[from:to]}
	if to < len(match) {
		out.NextToken = aws.String(strconv.Itoa(to))
	}
	return out, nil
}

func (f *fakeCW) GetMetricData(context.Context, *cloudwatch.GetMetricDataInput, ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error) {
	return &cloudwatch.GetMetricDataOutput{}, nil
}

// More than 100 alarms: every firing one is kept, quiet ones are dropped
// first, even when the firing ones sort last by name.
func TestAlarmsCapKeepsEveryFiringAlarm(t *testing.T) {
	cw := &fakeCW{}
	for i := range 130 {
		cw.alarms = append(cw.alarms, cwtypes.MetricAlarm{AlarmName: aws.String(fmt.Sprintf("a-ok-%03d", i)), StateValue: cwtypes.StateValueOk})
	}
	cw.alarms = append(cw.alarms,
		cwtypes.MetricAlarm{AlarmName: aws.String("m-no-data"), StateValue: cwtypes.StateValueInsufficientData},
		cwtypes.MetricAlarm{AlarmName: aws.String("z-api-5xx"), StateValue: cwtypes.StateValueAlarm},
		cwtypes.MetricAlarm{AlarmName: aws.String("z-db-cpu"), StateValue: cwtypes.StateValueAlarm},
	)
	c := &awsClient{cw: cw}
	al, err := c.Alarms(context.Background())
	require.NoError(t, err)
	require.Len(t, al, maxAlarms)
	require.Equal(t, []cwtypes.StateValue{cwtypes.StateValueAlarm, cwtypes.StateValueInsufficientData, cwtypes.StateValueOk}, cw.asked)
	sortAlarms(al)
	require.Equal(t, "z-api-5xx", al[0].Name)
	require.Equal(t, "z-db-cpu", al[1].Name)
	require.Equal(t, api.AdminSystemAlarmStateInsufficientData, al[2].State)
	require.Equal(t, api.AdminSystemAlarmStateOk, al[maxAlarms-1].State)
}

// fakeEB answers the Beanstalk reads; failing names an environment whose
// resources cannot be read.
type fakeEB struct {
	failing string
}

func (f *fakeEB) DescribeEnvironments(context.Context, *elasticbeanstalk.DescribeEnvironmentsInput, ...func(*elasticbeanstalk.Options)) (*elasticbeanstalk.DescribeEnvironmentsOutput, error) {
	return &elasticbeanstalk.DescribeEnvironmentsOutput{Environments: []ebtypes.EnvironmentDescription{
		{EnvironmentName: aws.String("todayz-prod-api"), Health: ebtypes.EnvironmentHealthGreen, Status: ebtypes.EnvironmentStatusReady, VersionLabel: aws.String("v42")},
		{EnvironmentName: aws.String("todayz-prod-worker"), Health: ebtypes.EnvironmentHealthYellow, Status: ebtypes.EnvironmentStatusReady, VersionLabel: aws.String("v42")},
	}}, nil
}

func (f *fakeEB) DescribeEnvironmentResources(_ context.Context, in *elasticbeanstalk.DescribeEnvironmentResourcesInput, _ ...func(*elasticbeanstalk.Options)) (*elasticbeanstalk.DescribeEnvironmentResourcesOutput, error) {
	if aws.ToString(in.EnvironmentName) == f.failing {
		return nil, &smithy.GenericAPIError{Code: "Throttling", Message: "Rate exceeded for arn:aws:iam::123456789012:role/x"}
	}
	return &elasticbeanstalk.DescribeEnvironmentResourcesOutput{EnvironmentResources: &ebtypes.EnvironmentResourceDescription{
		Instances:     []ebtypes.Instance{{Id: aws.String("i-1")}, {Id: aws.String("i-2")}},
		LoadBalancers: []ebtypes.LoadBalancer{{Name: aws.String("arn:aws:elasticloadbalancing:ap-south-1:123456789012:loadbalancer/app/awseb-api/1")}},
	}}, nil
}

func (f *fakeEB) DescribeEvents(context.Context, *elasticbeanstalk.DescribeEventsInput, ...func(*elasticbeanstalk.Options)) (*elasticbeanstalk.DescribeEventsOutput, error) {
	return &elasticbeanstalk.DescribeEventsOutput{Events: []ebtypes.EventDescription{
		{EventDate: aws.Time(t0), Message: aws.String("Environment update completed successfully."), VersionLabel: aws.String("v42")},
	}}, nil
}

// One environment that cannot be read is marked with its error class; the
// healthy one keeps its figures, and the panel and the ALB stand.
func TestOneEnvironmentFailingDegradesAlone(t *testing.T) {
	c := &awsClient{eb: &fakeEB{failing: "todayz-prod-worker"}}
	envs, err := c.Environments(context.Background())
	require.NoError(t, err)
	require.Len(t, envs, 2)
	byName := map[string]Environment{}
	for _, e := range envs {
		byName[e.Name] = e
	}
	api0, worker := byName["todayz-prod-api"], byName["todayz-prod-worker"]
	require.Nil(t, api0.ErrorClass)
	require.Equal(t, ptr(2), api0.InstanceCount)
	require.Equal(t, []string{"app/awseb-api/1"}, api0.LoadBalancers)
	require.Len(t, api0.Deploys, 1)
	require.Equal(t, ptr("Throttling"), worker.ErrorClass, "the class only, never the message")
	require.Nil(t, worker.InstanceCount)
	require.Equal(t, api.Yellow, worker.Health, "what DescribeEnvironments said still shows")

	f := healthyAWS()
	f.envs = envs
	out := New(Deps{Env: api.Prod, Clock: clock.NewFake(t0), AWS: f}).System(context.Background())
	require.Equal(t, api.AdminSystemPanelStatusOk, out.Beanstalk.Status)
	require.Len(t, out.Beanstalk.Environments, 2)
	require.Equal(t, ptr("Throttling"), out.Beanstalk.Environments[1].ErrorClass)
	require.Equal(t, api.AdminSystemPanelStatusOk, out.Alb.Status)
	require.Equal(t, []string{"app/awseb-api/1"}, f.albLBs)
}

// Without an authorizer the route fails closed: 500 for every caller, never
// the panels.
func TestNilAuthorizerFailsClosed(t *testing.T) {
	r := chi.NewRouter()
	Register(r, NewFromConfig(&config.Config{Env: config.EnvLocal}, nil, clock.NewFake(t0)), nil)
	for _, role := range []string{"super_admin", "admin", "support"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/system", nil)
		p := authn.Principal{UserID: ids.New(t0), SessionID: ids.New(t0), App: authn.AppAdmin, AMR: authn.AMROTP, Roles: []string{role}}
		req = req.WithContext(authn.WithPrincipal(req.Context(), p))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusInternalServerError, rec.Code, role)
		require.NotContains(t, rec.Body.String(), "beanstalk", role)
	}
}

func TestShownStatement(t *testing.T) {
	for in, want := range map[string]string{
		"SELECT $1 /* user=9876543210 */":                       "SELECT $1",
		"SELECT a FROM t /* outer /* inner */ still */ WHERE b": "SELECT a FROM t   WHERE b",
		"-- name: GetStore :one\nSELECT * FROM stores":          "-- name: GetStore :one\nSELECT * FROM stores",
		"-- phone 9876543210\nSELECT 1":                         "SELECT 1",
		"SELECT \"a/*b\" FROM t -- trailing tag=x":              "SELECT \"a/*b\" FROM t",
		"SELECT $1 /* cut mid-comment, tag=":                    "SELECT $1",
		"/* only a comment */":                                  "",
	} {
		require.Equal(t, want, shownStatement(in), in)
	}
	long := "SELECT " + fmt.Sprintf("%0600d", 0)
	require.Len(t, []rune(shownStatement(long)), maxStatementText)
}
