// Package ops serves A-22 System (GET /v1/admin/system, D-97): Beanstalk,
// the ALB, RDS and CloudWatch alarm states from AWS, and the database
// (pg_stat_statements top 10) and River panels from Postgres.
//
// AWS is read only in staging and prod, through the API's EC2 instance role
// (IMDSv2, never the SDK's default chain, see NewAWS). In local and test the
// service holds no AWS adapter at all: the AWS panels say not_configured and
// nothing calls AWS, whatever AWS_* variables the process has.
//
// The AWS panels are cached for AWSCacheTTL per API instance (one fetch at a
// time; a failed fetch is cached too, so a broken AWS is not asked on every
// request); the database and River panels for at most DBCacheTTL. A panel
// that fails carries status error and an error class (an AWS error code,
// Timeout, Credentials or Error); the other panels and the 200 stand.
package ops

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	api "github.com/todayz/backend/gen/api/admin-system"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
)

const (
	// AWSCacheTTL is how long one AWS fetch serves (A-22: "cached for 60 s").
	AWSCacheTTL = 60 * time.Second
	// DBCacheTTL bounds how stale the database and River panels may be (SC-OPS-04).
	DBCacheTTL = 5 * time.Second
	// fetchTimeout bounds one AWS fetch (every panel) and one database read.
	fetchTimeout = 10 * time.Second
	// albWindow is the ALB panel's window: the last complete 5-minute period.
	albWindow = 5 * time.Minute
	// albPeriodSec is albWindow in seconds, CloudWatch's Period.
	albPeriodSec int32 = 300
)

// Deps are what the service needs. AWS is nil off AWS (local, test): the AWS
// panels then say not_configured and nothing calls AWS. AWSErr, when set, is
// why the deployed adapter could not be built; every AWS panel then reports it.
type Deps struct {
	Env    api.AdminSystemEnv
	DB     *pgxpool.Pool
	Clock  clock.Clock
	AWS    AWS
	AWSErr error
}

// Service builds the A-22 response.
type Service struct {
	d Deps

	awsMu sync.Mutex
	aws   *awsSnapshot

	dbMu sync.Mutex
	db   *dbSnapshot
}

// New returns the service.
func New(d Deps) *Service {
	if d.Clock == nil {
		d.Clock = clock.Real{}
	}
	return &Service{d: d}
}

// System returns every panel.
func (s *Service) System(ctx context.Context) api.AdminSystem {
	a := s.awsPanels(ctx)
	db := s.dbPanels(ctx)
	now := s.d.Clock.Now() // after the panels, so no fetchedAt is in the future
	out := api.AdminSystem{
		Env:           s.d.Env,
		AwsConfigured: s.d.AWS != nil || s.d.AWSErr != nil,
		Beanstalk:     a.beanstalk,
		Alb:           a.alb,
		Rds:           a.rds,
		Alarms:        a.alarms,
		Database:      db.database,
		River:         db.river,
		ServerNow:     now,
	}
	out.Database.MultiAz = s.multiAZ(a)
	return out
}

// multiAZ is false off AWS (one local server, said as a fact), the RDS
// instance's answer on AWS when there is exactly one, and unknown otherwise.
func (s *Service) multiAZ(a *awsSnapshot) *bool {
	if !a.configured {
		f := false
		return &f
	}
	if a.rds.Status != api.AdminSystemPanelStatusOk || len(a.rds.Instances) != 1 {
		return nil
	}
	v := a.rds.Instances[0].MultiAz
	return &v
}

func errClassPtr(err error) *api.AdminSystemErrorClass {
	if err == nil {
		return nil
	}
	c := ErrorClass(err)
	return &c
}

func statusOf(err error) api.AdminSystemPanelStatus {
	if err != nil {
		return api.AdminSystemPanelStatusError
	}
	return api.AdminSystemPanelStatusOk
}

// NewFromConfig builds the service for the API: an AWS adapter only when
// APP_ENV is staging or prod (NewAWS), none in local and test, whatever
// AWS_* variables the process holds.
func NewFromConfig(cfg *config.Config, db *pgxpool.Pool, clk clock.Clock) *Service {
	d := Deps{Env: api.Local, DB: db, Clock: clk}
	if cfg == nil {
		return New(d)
	}
	d.Env = api.AdminSystemEnv(cfg.Env)
	if cfg.Env.IsDeployed() {
		d.AWS, d.AWSErr = NewAWS(cfg)
	}
	return New(d)
}
