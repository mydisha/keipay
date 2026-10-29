package worker

import (
	"fmt"

	"github.com/hibiken/asynq"
)

// Worker counts per queue. Each queue has its own asynq server, so
// reconciliation runs, which may take up to an hour each, can never occupy
// the workers that forward webhooks; runs beyond ReconciliationConcurrency
// wait in their queue.
const (
	WebhookConcurrency        = 10
	ReconciliationConcurrency = 2
)

// Handlers are the task handlers a Pool serves.
type Handlers struct {
	WebhookForward    asynq.HandlerFunc
	ReconciliationRun asynq.HandlerFunc
}

// Pool is the set of running asynq servers, one per queue.
type Pool struct {
	servers []*asynq.Server
}

// StartPool starts one server per queue. If one fails to start, those
// already started are shut down.
func StartPool(redisOpt asynq.RedisConnOpt, h Handlers, logger asynq.Logger) (*Pool, error) {
	queues := []struct {
		name, taskType string
		concurrency    int
		handler        asynq.HandlerFunc
	}{
		{QueueWebhooks, TypeWebhookForward, WebhookConcurrency, h.WebhookForward},
		{QueueReconciliation, TypeReconciliationRun, ReconciliationConcurrency, h.ReconciliationRun},
	}
	p := &Pool{}
	for _, q := range queues {
		srv := asynq.NewServer(redisOpt, asynq.Config{
			Concurrency: q.concurrency,
			Queues:      map[string]int{q.name: 1},
			Logger:      logger,
		})
		mux := asynq.NewServeMux()
		mux.HandleFunc(q.taskType, q.handler)
		if err := srv.Start(mux); err != nil {
			p.Shutdown()
			return nil, fmt.Errorf("start %s worker: %w", q.name, err)
		}
		p.servers = append(p.servers, srv)
	}
	return p, nil
}

// Shutdown stops every server, waiting for running tasks up to asynq's
// shutdown timeout.
func (p *Pool) Shutdown() {
	for _, srv := range p.servers {
		srv.Shutdown()
	}
}
