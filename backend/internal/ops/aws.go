package ops

import (
	"context"
	"sort"
	"sync"
	"time"

	api "github.com/todayz/backend/gen/api/admin-system"
)

// AWS is what A-22 reads from AWS, one method per panel. The deployed
// implementation is NewAWS; tests use a fake. Every method is read-only.
type AWS interface {
	// Environments lists the Beanstalk environments with their last deploys
	// and the load balancers they use (LoadBalancers feeds ALB). An error is
	// the list itself failing; one environment that cannot be read carries
	// its own ErrorClass instead.
	Environments(ctx context.Context) ([]Environment, error)
	// ALB sums the given load balancers over [start, end).
	ALB(ctx context.Context, loadBalancers []string, start, end time.Time) (ALBStats, error)
	// RDS lists the Postgres instances with their latest CPU, connections and
	// free storage.
	RDS(ctx context.Context, now time.Time) ([]api.AdminSystemRdsInstance, error)
	// Alarms lists every CloudWatch alarm's state.
	Alarms(ctx context.Context) ([]api.AdminSystemAlarm, error)
}

// Environment is one Beanstalk environment. LoadBalancers are CloudWatch
// LoadBalancer dimension values (app/<name>/<id>); they never leave the API.
type Environment struct {
	api.AdminSystemEnvironment
	LoadBalancers []string
}

// ALBStats are the ALB figures for a window; nil when there was no data.
type ALBStats struct {
	Requests     *float64
	HTTP5xx      *float64
	P95TargetSec *float64
}

type awsSnapshot struct {
	configured bool
	at         time.Time
	beanstalk  api.AdminSystemBeanstalk
	alb        api.AdminSystemAlb
	rds        api.AdminSystemRds
	alarms     api.AdminSystemAlarms
}

// awsPanels returns the cached AWS panels, fetching when the cache is older
// than AWSCacheTTL. One fetch runs at a time; callers meanwhile wait for it.
func (s *Service) awsPanels(ctx context.Context) *awsSnapshot {
	s.awsMu.Lock()
	defer s.awsMu.Unlock()
	// Read under the lock: a caller that waited for a fetch must see it as
	// fresh, not as newer than a clock reading taken before it (which counts
	// as stale and would refetch once per waiter).
	now := s.d.Clock.Now()
	if s.aws != nil && fresh(now, s.aws.at, AWSCacheTTL) {
		return s.aws
	}
	switch {
	case s.d.AWS != nil:
		// Detached from the request: a caller that goes away must not cache a
		// cancelled fetch for everyone for a minute.
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
		defer cancel()
		s.aws = fetchAWS(c, s.d.AWS, now)
	case s.d.AWSErr != nil:
		s.aws = failedAWS(now, s.d.AWSErr)
	default:
		s.aws = notConfigured(now)
	}
	return s.aws
}

// fresh is at no older than ttl at now; a clock that went back counts as stale.
func fresh(now, at time.Time, ttl time.Duration) bool {
	age := now.Sub(at)
	return age >= 0 && age < ttl
}

func emptyALB(now time.Time, status api.AdminSystemPanelStatus, err error) api.AdminSystemAlb {
	return api.AdminSystemAlb{Status: status, ErrorClass: errClassPtr(err), FetchedAt: now, WindowMinutes: int(albWindow / time.Minute)}
}

func panels(now time.Time, status api.AdminSystemPanelStatus, err error) *awsSnapshot {
	ec := errClassPtr(err)
	return &awsSnapshot{
		configured: status != api.AdminSystemPanelStatusNotConfigured,
		at:         now,
		beanstalk:  api.AdminSystemBeanstalk{Status: status, ErrorClass: ec, FetchedAt: now, Environments: []api.AdminSystemEnvironment{}},
		alb:        emptyALB(now, status, err),
		rds:        api.AdminSystemRds{Status: status, ErrorClass: ec, FetchedAt: now, Instances: []api.AdminSystemRdsInstance{}},
		alarms:     api.AdminSystemAlarms{Status: status, ErrorClass: ec, FetchedAt: now, Items: []api.AdminSystemAlarm{}},
	}
}

// notConfigured is every AWS panel off AWS: no call was made.
func notConfigured(now time.Time) *awsSnapshot {
	return panels(now, api.AdminSystemPanelStatusNotConfigured, nil)
}

// failedAWS is every AWS panel when the deployed adapter could not be built.
func failedAWS(now time.Time, err error) *awsSnapshot {
	return panels(now, api.AdminSystemPanelStatusError, err)
}

// fetchAWS reads every AWS panel: Beanstalk first (the ALB panel needs its
// load balancers), then the ALB, RDS and alarms at once. A failed panel
// leaves the others standing.
func fetchAWS(ctx context.Context, a AWS, now time.Time) *awsSnapshot {
	out := panels(now, api.AdminSystemPanelStatusOk, nil)

	envs, envErr := a.Environments(ctx)
	out.beanstalk.Status, out.beanstalk.ErrorClass = statusOf(envErr), errClassPtr(envErr)
	var lbs []string
	for _, e := range envs {
		out.beanstalk.Environments = append(out.beanstalk.Environments, e.AdminSystemEnvironment)
		lbs = append(lbs, e.LoadBalancers...)
	}
	sort.Slice(out.beanstalk.Environments, func(i, j int) bool {
		return out.beanstalk.Environments[i].Name < out.beanstalk.Environments[j].Name
	})

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		if envErr != nil {
			// No load balancers to ask about: the ALB panel shares Beanstalk's failure.
			out.alb = emptyALB(now, api.AdminSystemPanelStatusError, envErr)
			return
		}
		end := now.Truncate(albWindow)
		st, err := a.ALB(ctx, lbs, end.Add(-albWindow), end)
		out.alb = albPanel(now, st, err)
	}()
	go func() {
		defer wg.Done()
		inst, err := a.RDS(ctx, now)
		out.rds.Status, out.rds.ErrorClass = statusOf(err), errClassPtr(err)
		if err == nil {
			sort.Slice(inst, func(i, j int) bool { return inst[i].Name < inst[j].Name })
			out.rds.Instances = inst
		}
	}()
	go func() {
		defer wg.Done()
		al, err := a.Alarms(ctx)
		out.alarms.Status, out.alarms.ErrorClass = statusOf(err), errClassPtr(err)
		if err == nil {
			sortAlarms(al)
			out.alarms.Items = al
		}
	}()
	wg.Wait()
	return out
}

func albPanel(now time.Time, st ALBStats, err error) api.AdminSystemAlb {
	p := emptyALB(now, statusOf(err), err)
	if err != nil {
		return p
	}
	if st.Requests != nil {
		rpm := *st.Requests / albWindow.Minutes()
		p.RequestsPerMinute = &rpm
		if *st.Requests > 0 {
			var errs float64
			if st.HTTP5xx != nil {
				errs = *st.HTTP5xx
			}
			bp := min(int(errs*10000 / *st.Requests + 0.5), 10000)
			p.Http5xxRateBp = &bp
		}
	}
	if st.P95TargetSec != nil {
		ms := int(*st.P95TargetSec*1000 + 0.5)
		p.P95Ms = &ms
	}
	return p
}

var alarmOrder = map[api.AdminSystemAlarmState]int{
	api.AdminSystemAlarmStateAlarm:            0,
	api.AdminSystemAlarmStateInsufficientData: 1,
	api.AdminSystemAlarmStateOk:               2,
}

func sortAlarms(al []api.AdminSystemAlarm) {
	sort.Slice(al, func(i, j int) bool {
		if alarmOrder[al[i].State] != alarmOrder[al[j].State] {
			return alarmOrder[al[i].State] < alarmOrder[al[j].State]
		}
		return al[i].Name < al[j].Name
	})
}
