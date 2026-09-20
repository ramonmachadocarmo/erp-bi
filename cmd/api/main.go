package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"erp/pkg/config"
	"erp/pkg/httpserver"
	"erp/pkg/postgres"
	httpadapter "erp/services/bi-service/internal/adapters/http"
	pgadapter "erp/services/bi-service/internal/adapters/postgres"
	purchasingclient "erp/services/bi-service/internal/adapters/purchasing"
	salesclient "erp/services/bi-service/internal/adapters/sales"
	stockclient "erp/services/bi-service/internal/adapters/stock"
	"erp/services/bi-service/internal/application"
	"erp/services/bi-service/migrations"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := postgres.EnsureDatabase(ctx, cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.User, cfg.Postgres.Password, cfg.Postgres.DB); err != nil {
		log.Fatal(err)
	}
	pool, err := postgres.Connect(ctx, cfg.Postgres.DSN())
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool, migrations.FS, "."); err != nil {
		log.Fatal(err)
	}

	repo := pgadapter.New(pool)
	svc := application.New(
		pgadapter.Forecasts{Repo: repo},
		pgadapter.Budgets{Repo: repo},
		pgadapter.Schedules{Repo: repo},
		pgadapter.SupplierPrices{Repo: repo},
		salesclient.New(cfg.SalesBaseURL),
		stockclient.New(cfg.StockBaseURL),
		purchasingclient.New(cfg.PurchasingBaseURL),
		postgres.NewSequence(pool, "budget"),
	).WithScenarios(pgadapter.Scenarios{Repo: repo})

	go application.RunScheduler(ctx, svc, 60*time.Second)

	engine := httpserver.New(cfg.ServiceName)
	httpadapter.New(svc).Register(engine, httpserver.JWT(cfg.JWTSecret, cfg.JWTIssuer))

	srv := &http.Server{Addr: ":" + cfg.HTTPPort, Handler: engine}
	go func() {
		log.Printf("%s listening on %s", cfg.ServiceName, srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
}
