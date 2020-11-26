package driver

import (
	"context"
	"strings"
	"time"

	"github.com/ory/x/errorsx"

	"github.com/luna-duclos/instrumentedsql"
	"github.com/luna-duclos/instrumentedsql/opentracing"

	"github.com/ory/hydra/driver/configuration"

	"github.com/ory/x/resilience"

	"github.com/gobuffalo/pop/v5"
	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/ory/hydra/persistence/sql"

	"github.com/jmoiron/sqlx"

	"github.com/ory/x/dbal"
	"github.com/ory/x/sqlcon"

	"github.com/ory/hydra/client"
	"github.com/ory/hydra/consent"
	"github.com/ory/hydra/jwk"
	"github.com/ory/hydra/x"
)

type RegistrySQL struct {
	*RegistryBase
	db *sqlx.DB
}

var _ Registry = new(RegistrySQL)

func init() {
	dbal.RegisterDriver(func() dbal.Driver {
		return NewRegistrySQL()
	})
}

func NewRegistrySQL() *RegistrySQL {
	r := &RegistrySQL{
		RegistryBase: new(RegistryBase),
	}
	r.RegistryBase.with(r)
	return r
}

func (m *RegistrySQL) Init() error {
	if m.persister == nil {
		var opts []instrumentedsql.Opt
		if m.Tracer().IsLoaded() {
			opts = []instrumentedsql.Opt{
				instrumentedsql.WithTracer(opentracing.NewTracer(true)),
				instrumentedsql.WithOmitArgs(),
			}
		}

		// new db connection
		pool, idlePool, connMaxLifetime, cleanedDSN := sqlcon.ParseConnectionOptions(m.l, m.C.DSN())
		c, err := pop.NewConnection(&pop.ConnectionDetails{
			URL:                       sqlcon.FinalizeDSN(m.l, cleanedDSN),
			IdlePool:                  idlePool,
			ConnMaxLifetime:           connMaxLifetime,
			Pool:                      pool,
			UseInstrumentedDriver:     m.Tracer().IsLoaded(),
			InstrumentedDriverOptions: opts,
		})
		if err != nil {
			return errorsx.WithStack(err)
		}
		if err := resilience.Retry(m.l, 5*time.Second, 5*time.Minute, c.Open); err != nil {
			return errorsx.WithStack(err)
		}
		m.persister, err = sql.NewPersister(c, m, m.C, m.l)
		if err != nil {
			return err
		}

		// if dsn is memory we have to run the migrations on every start
		if m.C.DSN() == configuration.DefaultSQLiteMemoryDSN {
			m.Logger().Print("Hydra is running migrations on every startup as DSN is memory.\n")
			m.Logger().Print("This means your data is lost when Hydra terminates.\n")
			if err := m.persister.MigrateUp(context.Background()); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *RegistrySQL) alwaysCanHandle(dsn string) bool {
	scheme := strings.Split(dsn, "://")[0]
	s := dbal.Canonicalize(scheme)
	return s == dbal.DriverMySQL || s == dbal.DriverPostgreSQL || s == dbal.DriverCockroachDB
}

func (m *RegistrySQL) Ping() error {
	return m.Persister().Connection(context.Background()).Open()
}

func (m *RegistrySQL) ClientManager() client.Manager {
	return m.Persister()
}

func (m *RegistrySQL) ConsentManager() consent.Manager {
	return m.Persister()
}

func (m *RegistrySQL) OAuth2Storage() x.FositeStorer {
	return m.Persister()
}

func (m *RegistrySQL) KeyManager() jwk.Manager {
	return m.Persister()
}
