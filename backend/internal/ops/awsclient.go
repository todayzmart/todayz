package ops

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk"
	"github.com/aws/aws-sdk-go-v2/service/rds"

	api "github.com/todayz/backend/gen/api/admin-system"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
)

const (
	// imdsEndpoint is pinned so AWS_EC2_METADATA_SERVICE_ENDPOINT cannot move it.
	imdsEndpoint = "http://169.254.169.254"
	// imdsTimeout bounds one metadata request (off EC2 it fails fast).
	imdsTimeout = 2 * time.Second
	// callTimeout bounds one AWS API request.
	callTimeout = 5 * time.Second
	// maxEnvironments, maxInstances and maxAlarms bound what one fetch reads.
	maxEnvironments = 20
	maxInstances    = 20
	maxAlarms       = 100
	maxDeploys      = 10
	// eventsPerEnvironment is how many Beanstalk events are scanned for deploys.
	eventsPerEnvironment = 100
)

// ErrNotDeployed is NewAWS outside staging and prod: no AWS client is built there.
var ErrNotDeployed = errors.New("ops: AWS is read only in staging and prod")

// ErrNoRegion is NewAWS in staging or prod without AWS_REGION.
var ErrNoRegion = errors.New("ops: AWS_REGION is not set")

// awsClient reads Beanstalk, CloudWatch and RDS with the instance role. Every
// call is a read: Describe*, GetMetricData (the IAM policy is W1-10's).
type awsClient struct {
	eb  *elasticbeanstalk.Client
	cw  *cloudwatch.Client
	rds *rds.Client
}

// NewAWS builds the deployed adapter. It refuses outside staging and prod, so
// local and test never hold an AWS client whatever AWS_* variables are set
// (SC-OPS-09). Credentials are the EC2 instance role through IMDSv2 only, on
// the pinned metadata address and never through a proxy; there is no
// default chain, no ~/.aws and no environment keys. Every request goes
// through httpx.NewOutboundClient (egress guard).
func NewAWS(cfg *config.Config) (AWS, error) {
	c, err := newAWS(cfg, nil)
	if err != nil {
		return nil, err // a nil interface, not a typed nil *awsClient
	}
	return c, nil
}

type wrapTransport func(kind string, next http.RoundTripper) http.RoundTripper

func newAWS(cfg *config.Config, wrap wrapTransport) (*awsClient, error) {
	if cfg == nil || !cfg.Env.IsDeployed() {
		return nil, ErrNotDeployed
	}
	if cfg.AWSRegion == "" {
		return nil, ErrNoRegion
	}
	httpClient := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: cfg.NoEgress, Timeout: callTimeout})
	imdsClient := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: cfg.NoEgress, Timeout: imdsTimeout, NoProxy: true})
	if wrap != nil {
		httpClient.Transport = wrap("aws", httpClient.Transport)
		imdsClient.Transport = wrap("imds", imdsClient.Transport)
	}
	creds := aws.NewCredentialsCache(tagCredentials{ec2rolecreds.New(func(eo *ec2rolecreds.Options) {
		eo.Client = imds.New(imds.Options{
			HTTPClient:        imdsClient,
			Endpoint:          imdsEndpoint,
			ClientEnableState: imds.ClientEnabled,
			EnableFallback:    aws.FalseTernary, // IMDSv2 tokens only
		})
	})})
	// The next fetch is a minute away; one bounded attempt per call is enough.
	retryer := func() aws.Retryer { return aws.NopRetryer{} }
	return &awsClient{
		eb: elasticbeanstalk.New(elasticbeanstalk.Options{
			Region: cfg.AWSRegion, HTTPClient: httpClient, Credentials: creds, Retryer: retryer(),
		}),
		cw: cloudwatch.New(cloudwatch.Options{
			Region: cfg.AWSRegion, HTTPClient: httpClient, Credentials: creds, Retryer: retryer(),
		}),
		rds: rds.New(rds.Options{
			Region: cfg.AWSRegion, HTTPClient: httpClient, Credentials: creds, Retryer: retryer(),
		}),
	}, nil
}

// tagCredentials marks a failure to get the role's credentials, so the panel
// says Credentials rather than the call's error.
type tagCredentials struct{ next aws.CredentialsProvider }

func (t tagCredentials) Retrieve(ctx context.Context) (aws.Credentials, error) {
	c, err := t.next.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, &credentialsError{cause: err}
	}
	return c, nil
}

// credentialsError never prints its cause (the SDK text can name the role).
type credentialsError struct{ cause error }

func (e *credentialsError) Error() string { return "ops: instance role credentials unavailable" }
func (e *credentialsError) Unwrap() error { return e.cause }

func (c *awsClient) Environments(ctx context.Context) ([]Environment, error) {
	out, err := c.eb.DescribeEnvironments(ctx, &elasticbeanstalk.DescribeEnvironmentsInput{IncludeDeleted: aws.Bool(false)})
	if err != nil {
		return nil, err
	}
	descs := out.Environments
	if len(descs) > maxEnvironments {
		descs = descs[:maxEnvironments]
	}
	envs := make([]Environment, len(descs))
	errs := make([]error, len(descs))
	var wg sync.WaitGroup
	for i, d := range descs {
		name := aws.ToString(d.EnvironmentName)
		envs[i] = Environment{AdminSystemEnvironment: api.AdminSystemEnvironment{
			Name:         name,
			Health:       health(string(d.Health)),
			Status:       string(d.Status),
			VersionLabel: d.VersionLabel,
			UpdatedAt:    d.DateUpdated,
			Deploys:      []api.AdminSystemDeploy{},
		}}
		wg.Add(1)
		go func(e *Environment, errp *error) {
			defer wg.Done()
			*errp = c.environmentDetail(ctx, e)
		}(&envs[i], &errs[i])
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return envs, nil
}

// environmentDetail fills the instance count, the load balancers and the
// last deploys of one environment.
func (c *awsClient) environmentDetail(ctx context.Context, e *Environment) error {
	res, err := c.eb.DescribeEnvironmentResources(ctx, &elasticbeanstalk.DescribeEnvironmentResourcesInput{EnvironmentName: aws.String(e.Name)})
	if err != nil {
		return err
	}
	if r := res.EnvironmentResources; r != nil {
		n := len(r.Instances)
		e.InstanceCount = &n
		for _, lb := range r.LoadBalancers {
			if dim := albDimension(aws.ToString(lb.Name)); dim != "" {
				e.LoadBalancers = append(e.LoadBalancers, dim)
			}
		}
	}
	ev, err := c.eb.DescribeEvents(ctx, &elasticbeanstalk.DescribeEventsInput{
		EnvironmentName: aws.String(e.Name),
		MaxRecords:      aws.Int32(eventsPerEnvironment),
	})
	if err != nil {
		return err
	}
	for _, x := range ev.Events {
		outcome, ok := deployOutcome(aws.ToString(x.Message))
		if !ok || x.EventDate == nil {
			continue
		}
		e.Deploys = append(e.Deploys, api.AdminSystemDeploy{At: x.EventDate.UTC(), VersionLabel: x.VersionLabel, Outcome: outcome})
		if len(e.Deploys) == maxDeploys {
			break
		}
	}
	return nil
}

// health maps Beanstalk's health colour.
func health(h string) api.AdminSystemHealth {
	switch strings.ToLower(h) {
	case "green":
		return api.Green
	case "yellow":
		return api.Yellow
	case "red":
		return api.Red
	case "grey":
		return api.Grey
	}
	return api.Unknown
}

// deployOutcome reads how an environment update ended from Beanstalk's fixed
// event phrases (newest events come first). Other events are not deploys.
func deployOutcome(msg string) (api.AdminSystemDeployOutcome, bool) {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "environment update completed successfully"):
		return api.Succeeded, true
	case strings.Contains(m, "failed to deploy application"),
		strings.Contains(m, "update environment operation is complete, but with errors"),
		strings.Contains(m, "environment update failed"):
		return api.Failed, true
	}
	return "", false
}

// albDimension turns an ALB's ARN (or its app/<name>/<id> suffix) into the
// CloudWatch LoadBalancer dimension; a classic ELB name gives "".
func albDimension(name string) string {
	if i := strings.Index(name, ":loadbalancer/"); i >= 0 {
		name = name[i+len(":loadbalancer/"):]
	}
	if strings.HasPrefix(name, "app/") {
		return name
	}
	return ""
}

func (c *awsClient) ALB(ctx context.Context, lbs []string, start, end time.Time) (ALBStats, error) {
	if len(lbs) == 0 {
		return ALBStats{}, nil
	}
	var qs []cwtypes.MetricDataQuery
	for i, lb := range lbs {
		dims := []cwtypes.Dimension{{Name: aws.String("LoadBalancer"), Value: aws.String(lb)}}
		qs = append(qs,
			metricQuery(fmt.Sprintf("req%d", i), "AWS/ApplicationELB", "RequestCount", "Sum", dims, albPeriodSec),
			metricQuery(fmt.Sprintf("tgt%d", i), "AWS/ApplicationELB", "HTTPCode_Target_5XX_Count", "Sum", dims, albPeriodSec),
			metricQuery(fmt.Sprintf("elb%d", i), "AWS/ApplicationELB", "HTTPCode_ELB_5XX_Count", "Sum", dims, albPeriodSec),
			metricQuery(fmt.Sprintf("p95%d", i), "AWS/ApplicationELB", "TargetResponseTime", "p95", dims, albPeriodSec),
		)
	}
	vals, err := c.metrics(ctx, qs, start, end)
	if err != nil {
		return ALBStats{}, err
	}
	var st ALBStats
	for i := range lbs {
		st.Requests = addTo(st.Requests, vals[fmt.Sprintf("req%d", i)])
		st.HTTP5xx = addTo(st.HTTP5xx, vals[fmt.Sprintf("tgt%d", i)])
		st.HTTP5xx = addTo(st.HTTP5xx, vals[fmt.Sprintf("elb%d", i)])
		// p95 does not add up across load balancers: the worst one stands.
		if v, ok := vals[fmt.Sprintf("p95%d", i)]; ok && (st.P95TargetSec == nil || v > *st.P95TargetSec) {
			st.P95TargetSec = &v
		}
	}
	return st, nil
}

func addTo(sum *float64, v float64) *float64 {
	if sum == nil {
		return &v
	}
	s := *sum + v
	return &s
}

func metricQuery(id, ns, name, stat string, dims []cwtypes.Dimension, periodSec int32) cwtypes.MetricDataQuery {
	return cwtypes.MetricDataQuery{
		Id: aws.String(id),
		MetricStat: &cwtypes.MetricStat{
			Metric: &cwtypes.Metric{Namespace: aws.String(ns), MetricName: aws.String(name), Dimensions: dims},
			Period: aws.Int32(periodSec),
			Stat:   aws.String(stat),
		},
		ReturnData: aws.Bool(true),
	}
}

// metrics runs the queries and returns each one's newest value; a query with
// no data is absent.
func (c *awsClient) metrics(ctx context.Context, qs []cwtypes.MetricDataQuery, start, end time.Time) (map[string]float64, error) {
	out := map[string]float64{}
	p := cloudwatch.NewGetMetricDataPaginator(c.cw, &cloudwatch.GetMetricDataInput{
		MetricDataQueries: qs,
		StartTime:         aws.Time(start),
		EndTime:           aws.Time(end),
		ScanBy:            cwtypes.ScanByTimestampDescending,
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range page.MetricDataResults {
			id := aws.ToString(r.Id)
			if _, seen := out[id]; !seen && len(r.Values) > 0 {
				out[id] = r.Values[0]
			}
		}
	}
	return out, nil
}

func (c *awsClient) RDS(ctx context.Context, now time.Time) ([]api.AdminSystemRdsInstance, error) {
	var insts []api.AdminSystemRdsInstance
	p := rds.NewDescribeDBInstancesPaginator(c.rds, &rds.DescribeDBInstancesInput{})
	for p.HasMorePages() && len(insts) < maxInstances {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, d := range page.DBInstances {
			if !strings.HasPrefix(aws.ToString(d.Engine), "postgres") || len(insts) == maxInstances {
				continue
			}
			insts = append(insts, api.AdminSystemRdsInstance{
				Name:               aws.ToString(d.DBInstanceIdentifier),
				Status:             aws.ToString(d.DBInstanceStatus),
				InstanceClass:      aws.ToString(d.DBInstanceClass),
				EngineVersion:      aws.ToString(d.EngineVersion),
				MultiAz:            aws.ToBool(d.MultiAZ),
				AllocatedStorageGb: int(aws.ToInt32(d.AllocatedStorage)),
			})
		}
	}
	if len(insts) == 0 {
		return []api.AdminSystemRdsInstance{}, nil
	}
	var qs []cwtypes.MetricDataQuery
	for i, in := range insts {
		dims := []cwtypes.Dimension{{Name: aws.String("DBInstanceIdentifier"), Value: aws.String(in.Name)}}
		qs = append(qs,
			metricQuery(fmt.Sprintf("cpu%d", i), "AWS/RDS", "CPUUtilization", "Average", dims, 60),
			metricQuery(fmt.Sprintf("conn%d", i), "AWS/RDS", "DatabaseConnections", "Maximum", dims, 60),
			metricQuery(fmt.Sprintf("free%d", i), "AWS/RDS", "FreeStorageSpace", "Minimum", dims, 60),
		)
	}
	end := now.Truncate(time.Minute)
	vals, err := c.metrics(ctx, qs, end.Add(-10*time.Minute), end)
	if err != nil {
		return nil, err
	}
	for i := range insts {
		if v, ok := vals[fmt.Sprintf("cpu%d", i)]; ok {
			v = min(max(v, 0), 100)
			insts[i].CpuPercent = &v
		}
		if v, ok := vals[fmt.Sprintf("conn%d", i)]; ok {
			n := int(max(v, 0) + 0.5)
			insts[i].Connections = &n
		}
		if v, ok := vals[fmt.Sprintf("free%d", i)]; ok {
			n := int64(max(v, 0))
			insts[i].FreeStorageBytes = &n
		}
	}
	return insts, nil
}

func (c *awsClient) Alarms(ctx context.Context) ([]api.AdminSystemAlarm, error) {
	out := []api.AdminSystemAlarm{}
	p := cloudwatch.NewDescribeAlarmsPaginator(c.cw, &cloudwatch.DescribeAlarmsInput{
		AlarmTypes: []cwtypes.AlarmType{cwtypes.AlarmTypeMetricAlarm, cwtypes.AlarmTypeCompositeAlarm},
		MaxRecords: aws.Int32(maxAlarms),
	})
	for p.HasMorePages() && len(out) < maxAlarms {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range page.MetricAlarms {
			out = appendAlarm(out, aws.ToString(a.AlarmName), string(a.StateValue), a.StateUpdatedTimestamp)
		}
		for _, a := range page.CompositeAlarms {
			out = appendAlarm(out, aws.ToString(a.AlarmName), string(a.StateValue), a.StateUpdatedTimestamp)
		}
	}
	return out, nil
}

func appendAlarm(out []api.AdminSystemAlarm, name, state string, at *time.Time) []api.AdminSystemAlarm {
	if len(out) == maxAlarms {
		return out
	}
	var s api.AdminSystemAlarmState
	switch state {
	case "ALARM":
		s = api.AdminSystemAlarmStateAlarm
	case "OK":
		s = api.AdminSystemAlarmStateOk
	default:
		s = api.AdminSystemAlarmStateInsufficientData
	}
	if at != nil {
		u := at.UTC()
		at = &u
	}
	return append(out, api.AdminSystemAlarm{Name: name, State: s, UpdatedAt: at})
}
