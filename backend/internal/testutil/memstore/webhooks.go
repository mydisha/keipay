package memstore

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Webhooks holds in-memory webhook endpoints, events, deliveries with their
// attempts, and inbound webhooks, with the unique rules of their tables.
// Endpoints, Events, Deliveries and Inbound return the repository views.
// Register Webhooks with Tx to roll all four back together.
type Webhooks struct {
	mu        sync.Mutex
	endpoints map[int64]domain.WebhookEndpoint
	events    map[int64]domain.Event
	deliv     map[int64]domain.WebhookDelivery
	attempts  []domain.DeliveryAttempt
	inbound   map[int64]domain.InboundWebhook
	nextID    int64
	apps      *Apps
	payments  *Payments
	conns     *Connections
	clock     func() time.Time

	// CreateDeliveryErr, when set, is returned by Deliveries().Create.
	CreateDeliveryErr error
	// InboundCreateErr, when set, is returned by Inbound().Create.
	InboundCreateErr error
	// ListActiveCalls counts Endpoints().ListActiveByApp calls.
	ListActiveCalls int
}

// NewWebhooks returns empty stores. apps fills app summaries, payments the
// payment public ids and conns (optional) the connection summaries of reads.
func NewWebhooks(apps *Apps, payments *Payments, conns *Connections, now func() time.Time) *Webhooks {
	return &Webhooks{
		endpoints: map[int64]domain.WebhookEndpoint{}, events: map[int64]domain.Event{},
		deliv: map[int64]domain.WebhookDelivery{}, inbound: map[int64]domain.InboundWebhook{},
		apps: apps, payments: payments, conns: conns, clock: now,
	}
}

func (w *Webhooks) snapshot() func() {
	w.mu.Lock()
	defer w.mu.Unlock()
	eps, evs, ds, in := maps.Clone(w.endpoints), maps.Clone(w.events), maps.Clone(w.deliv), maps.Clone(w.inbound)
	atts, next := slices.Clone(w.attempts), w.nextID
	return func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.endpoints, w.events, w.deliv, w.inbound, w.attempts, w.nextID = eps, evs, ds, in, atts, next
	}
}

func (w *Webhooks) id() int64 {
	w.nextID++
	return w.nextID
}

func (w *Webhooks) now() time.Time { return w.clock().UTC() }

func (w *Webhooks) appSummary(id int64) domain.AppSummary {
	if a, err := w.apps.GetByID(context.Background(), id); err == nil {
		return a.Summary()
	}
	return domain.AppSummary{}
}

// Endpoints is the domain.WebhookEndpointRepository view.
func (w *Webhooks) Endpoints() *WebhookEndpoints { return &WebhookEndpoints{w: w} }

// Events is the domain.EventRepository view.
func (w *Webhooks) Events() *Events { return &Events{w: w} }

// Deliveries is the domain.DeliveryRepository view.
func (w *Webhooks) Deliveries() *Deliveries { return &Deliveries{w: w} }

// Inbound is the domain.InboundWebhookRepository view.
func (w *Webhooks) Inbound() *InboundWebhooks { return &InboundWebhooks{w: w} }

// AllEndpoints returns every endpoint, deleted ones included, oldest first.
func (w *Webhooks) AllEndpoints() []domain.WebhookEndpoint {
	w.mu.Lock()
	defer w.mu.Unlock()
	return sortedByID(w.endpoints, func(e domain.WebhookEndpoint) int64 { return e.ID })
}

// AllEvents returns every event, oldest first.
func (w *Webhooks) AllEvents() []domain.Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	return sortedByID(w.events, func(e domain.Event) int64 { return e.ID })
}

// AllDeliveries returns every delivery with its references, oldest first.
func (w *Webhooks) AllDeliveries() []domain.WebhookDelivery {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := sortedByID(w.deliv, func(d domain.WebhookDelivery) int64 { return d.ID })
	for i := range out {
		w.fillDelivery(&out[i])
	}
	return out
}

// AllAttempts returns every attempt in insertion order.
func (w *Webhooks) AllAttempts() []domain.DeliveryAttempt {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.attempts)
}

// AllInbound returns every inbound webhook, oldest first.
func (w *Webhooks) AllInbound() []domain.InboundWebhook {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := sortedByID(w.inbound, func(v domain.InboundWebhook) int64 { return v.ID })
	for i := range out {
		w.fillInbound(&out[i])
	}
	return out
}

func sortedByID[T any](m map[int64]T, key func(T) int64) []T {
	out := slices.Collect(maps.Values(m))
	slices.SortFunc(out, func(a, b T) int { return int(key(a) - key(b)) })
	return out
}

// WebhookEndpoints is an in-memory domain.WebhookEndpointRepository.
type WebhookEndpoints struct{ w *Webhooks }

func cloneEndpoint(e domain.WebhookEndpoint) domain.WebhookEndpoint {
	e.Events = slices.Clone(e.Events)
	e.HeadersHint = slices.Clone(e.HeadersHint)
	return e
}

// Create inserts an endpoint.
func (s *WebhookEndpoints) Create(_ context.Context, e *domain.WebhookEndpoint) error {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	e.ID = w.id()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = w.now()
	}
	e.UpdatedAt = e.CreatedAt
	if e.HeadersHint == nil {
		e.HeadersHint = []domain.WebhookHeaderHint{}
	}
	w.endpoints[e.ID] = cloneEndpoint(*e)
	return nil
}

// Update replaces the stored endpoint.
func (s *WebhookEndpoints) Update(_ context.Context, e *domain.WebhookEndpoint) error {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.endpoints[e.ID]; !ok {
		return domain.ErrNotFound
	}
	e.UpdatedAt = w.now()
	w.endpoints[e.ID] = cloneEndpoint(*e)
	return nil
}

// UpdateHealth writes the counter and status fields.
func (s *WebhookEndpoints) UpdateHealth(_ context.Context, e *domain.WebhookEndpoint) error {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	cur, ok := w.endpoints[e.ID]
	if !ok {
		return domain.ErrNotFound
	}
	if cur.Status != e.Status {
		cur.UpdatedAt = w.now()
	}
	cur.ConsecutiveFailures, cur.Status, cur.DisabledReason, cur.DisabledAt = e.ConsecutiveFailures, e.Status, e.DisabledReason, e.DisabledAt
	w.endpoints[e.ID] = cur
	e.UpdatedAt = cur.UpdatedAt
	return nil
}

func (s *WebhookEndpoints) find(match func(domain.WebhookEndpoint) bool) (*domain.WebhookEndpoint, error) {
	w := s.w
	w.mu.Lock()
	var found *domain.WebhookEndpoint
	for _, e := range w.endpoints {
		if match(e) {
			c := cloneEndpoint(e)
			found = &c
			break
		}
	}
	w.mu.Unlock()
	if found == nil {
		return nil, domain.ErrNotFound
	}
	found.App = w.appSummary(found.AppID)
	return found, nil
}

// GetByPublicID loads an endpoint that is not deleted.
func (s *WebhookEndpoints) GetByPublicID(_ context.Context, publicID string) (*domain.WebhookEndpoint, error) {
	return s.find(func(e domain.WebhookEndpoint) bool { return e.PublicID == publicID && e.DeletedAt == nil })
}

// GetByPublicIDForUpdate is GetByPublicID.
func (s *WebhookEndpoints) GetByPublicIDForUpdate(ctx context.Context, publicID string) (*domain.WebhookEndpoint, error) {
	return s.GetByPublicID(ctx, publicID)
}

// GetByIDForUpdate loads an endpoint by internal id, deleted ones included.
func (s *WebhookEndpoints) GetByIDForUpdate(_ context.Context, id int64) (*domain.WebhookEndpoint, error) {
	return s.find(func(e domain.WebhookEndpoint) bool { return e.ID == id })
}

func (s *WebhookEndpoints) live(appID int64) []domain.WebhookEndpoint {
	w := s.w
	w.mu.Lock()
	var out []domain.WebhookEndpoint
	for _, e := range sortedByID(w.endpoints, func(e domain.WebhookEndpoint) int64 { return e.ID }) {
		if e.AppID == appID && e.DeletedAt == nil {
			out = append(out, cloneEndpoint(e))
		}
	}
	w.mu.Unlock()
	for i := range out {
		out[i].App = w.appSummary(out[i].AppID)
	}
	return out
}

// List returns one page of the app's endpoints that are not deleted.
func (s *WebhookEndpoints) List(_ context.Context, appID int64, p domain.Page) (domain.PageResult[domain.WebhookEndpoint], error) {
	return page(s.live(appID), p, func(e domain.WebhookEndpoint) domain.Cursor {
		return domain.Cursor{Time: e.CreatedAt, ID: e.PublicID}
	}), nil
}

// CountByApp counts the app's endpoints that are not deleted.
func (s *WebhookEndpoints) CountByApp(_ context.Context, appID int64) (int, error) {
	return len(s.live(appID)), nil
}

// ListActiveByApp returns the app's active endpoints that are not deleted.
func (s *WebhookEndpoints) ListActiveByApp(_ context.Context, appID int64) ([]domain.WebhookEndpoint, error) {
	s.w.mu.Lock()
	s.w.ListActiveCalls++
	s.w.mu.Unlock()
	var out []domain.WebhookEndpoint
	for _, e := range s.live(appID) {
		if e.Status == domain.WebhookEndpointActive {
			out = append(out, e)
		}
	}
	return out, nil
}

// Events is an in-memory domain.EventRepository.
type Events struct{ w *Webhooks }

// Create inserts an event.
func (s *Events) Create(_ context.Context, e *domain.Event) error {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	e.ID = w.id()
	w.events[e.ID] = *e
	return nil
}

// Deliveries is an in-memory domain.DeliveryRepository.
type Deliveries struct{ w *Webhooks }

// fillDelivery sets the joined fields of d; the caller holds w.mu.
func (w *Webhooks) fillDelivery(d *domain.WebhookDelivery) {
	d.App = w.appSummary(d.AppID)
	d.Event, d.Endpoint, d.PaymentPublicID = nil, nil, ""
	if d.EventID != nil {
		if e, ok := w.events[*d.EventID]; ok {
			d.Event = &domain.EventRef{PublicID: e.PublicID, Type: e.Type, IsTest: e.IsTest}
		}
	}
	if d.EndpointID != nil {
		if e, ok := w.endpoints[*d.EndpointID]; ok {
			ref := e.Ref()
			d.Endpoint = &ref
		}
	}
	if d.PaymentID != nil && w.payments != nil {
		for _, p := range w.payments.All() {
			if p.ID == *d.PaymentID {
				d.PaymentPublicID = p.PublicID
			}
		}
	}
}

// Create inserts a delivery.
func (s *Deliveries) Create(_ context.Context, d *domain.WebhookDelivery) error {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.CreateDeliveryErr != nil {
		return w.CreateDeliveryErr
	}
	for _, v := range w.deliv {
		if v.PublicID == d.PublicID {
			return domain.ErrConflict
		}
	}
	d.ID = w.id()
	d.CreatedAt = w.now()
	d.UpdatedAt = d.CreatedAt
	w.deliv[d.ID] = *d
	return nil
}

func (s *Deliveries) find(match func(domain.WebhookDelivery) bool) (*domain.WebhookDelivery, error) {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, d := range w.deliv {
		if match(d) {
			w.fillDelivery(&d)
			return &d, nil
		}
	}
	return nil, domain.ErrNotFound
}

// GetByPublicID loads a delivery by dlv_ id.
func (s *Deliveries) GetByPublicID(_ context.Context, publicID string) (*domain.WebhookDelivery, error) {
	return s.find(func(d domain.WebhookDelivery) bool { return d.PublicID == publicID })
}

// GetByPublicIDForUpdate is GetByPublicID.
func (s *Deliveries) GetByPublicIDForUpdate(ctx context.Context, publicID string) (*domain.WebhookDelivery, error) {
	return s.GetByPublicID(ctx, publicID)
}

// List returns one page of deliveries without their payload.
func (s *Deliveries) List(_ context.Context, f domain.DeliveryFilter, p domain.Page) (domain.PageResult[domain.WebhookDelivery], error) {
	var items []domain.WebhookDelivery
	for _, d := range s.w.AllDeliveries() {
		if !deliveryMatches(d, f) {
			continue
		}
		d.Payload = nil
		items = append(items, d)
	}
	return page(items, p, func(d domain.WebhookDelivery) domain.Cursor { return domain.Cursor{Time: d.CreatedAt, ID: d.PublicID} }), nil
}

func deliveryMatches(d domain.WebhookDelivery, f domain.DeliveryFilter) bool {
	switch {
	case f.AppPublicID != nil && d.App.PublicID != *f.AppPublicID,
		f.EndpointPublicID != nil && (d.Endpoint == nil || d.Endpoint.PublicID != *f.EndpointPublicID),
		f.EventPublicID != nil && (d.Event == nil || d.Event.PublicID != *f.EventPublicID),
		f.EventType != nil && d.EventType != string(*f.EventType),
		f.Status != nil && d.Status != *f.Status,
		f.Environment != nil && d.App.Environment != *f.Environment,
		f.From != nil && d.CreatedAt.Before(*f.From),
		f.To != nil && d.CreatedAt.After(*f.To):
		return false
	}
	return true
}

// Attempts returns the delivery's attempts by number.
func (s *Deliveries) Attempts(_ context.Context, deliveryID int64) ([]domain.DeliveryAttempt, error) {
	out := []domain.DeliveryAttempt{}
	for _, a := range s.w.AllAttempts() {
		if a.DeliveryID == deliveryID {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b domain.DeliveryAttempt) int { return a.AttemptNo - b.AttemptNo })
	return out, nil
}

// LoadForSend loads a delivery with its event reference and endpoint.
func (s *Deliveries) LoadForSend(_ context.Context, id int64) (*domain.WebhookDelivery, *domain.WebhookEndpoint, error) {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	d, ok := w.deliv[id]
	if !ok {
		return nil, nil, domain.ErrNotFound
	}
	w.fillDelivery(&d)
	if d.EndpointID == nil {
		return &d, nil, nil
	}
	e, ok := w.endpoints[*d.EndpointID]
	if !ok {
		return &d, nil, nil
	}
	e = cloneEndpoint(e)
	e.App = d.App
	return &d, &e, nil
}

// Requeue writes the replay state of d.
func (s *Deliveries) Requeue(_ context.Context, d *domain.WebhookDelivery) error {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	cur, ok := w.deliv[d.ID]
	if !ok {
		return domain.ErrNotFound
	}
	cur.Status, cur.MaxAttempts, cur.NextAttemptAt, cur.UpdatedAt = d.Status, d.MaxAttempts, d.NextAttemptAt, w.now()
	w.deliv[d.ID] = cur
	d.UpdatedAt = cur.UpdatedAt
	return nil
}

// Fail ends a PENDING delivery.
func (s *Deliveries) Fail(_ context.Context, id int64, lastErr string) (bool, error) {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	cur, ok := w.deliv[id]
	if !ok || cur.Status != domain.WebhookDeliveryPending {
		return false, nil
	}
	cur.Status, cur.LastError, cur.NextAttemptAt, cur.UpdatedAt = domain.WebhookDeliveryFailed, lastErr, nil, w.now()
	w.deliv[id] = cur
	return true, nil
}

// RecordAttempt stores a and the new state of d when d is still PENDING with
// prevAttempts attempts.
func (s *Deliveries) RecordAttempt(_ context.Context, d *domain.WebhookDelivery, prevAttempts int, a *domain.DeliveryAttempt) (bool, error) {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	cur, ok := w.deliv[d.ID]
	if !ok || cur.Status != domain.WebhookDeliveryPending || cur.Attempts != prevAttempts {
		return false, nil
	}
	cur.Status, cur.Attempts, cur.NextAttemptAt, cur.LastAttemptAt = d.Status, d.Attempts, d.NextAttemptAt, d.LastAttemptAt
	cur.LastResponseStatus, cur.LastError, cur.UpdatedAt = d.LastResponseStatus, d.LastError, w.now()
	w.deliv[d.ID] = cur
	a.ID = w.id()
	// The stored headers go through JSON like the jsonb column.
	b, _ := json.Marshal(a.RequestHeaders)
	stored := *a
	stored.RequestHeaders = nil
	_ = json.Unmarshal(b, &stored.RequestHeaders)
	w.attempts = append(w.attempts, stored)
	return true, nil
}

// ListDue returns PENDING deliveries due before olderThan, oldest first.
func (s *Deliveries) ListDue(_ context.Context, olderThan time.Time, limit int) ([]domain.WebhookDelivery, error) {
	var out []domain.WebhookDelivery
	for _, d := range s.w.AllDeliveries() {
		if d.Status == domain.WebhookDeliveryPending && d.NextAttemptAt != nil && d.NextAttemptAt.Before(olderThan) {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b domain.WebhookDelivery) int { return a.NextAttemptAt.Compare(*b.NextAttemptAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// InboundWebhooks is an in-memory domain.InboundWebhookRepository.
type InboundWebhooks struct{ w *Webhooks }

func claimed(r domain.InboundResult) bool {
	switch r {
	case domain.InboundApplied, domain.InboundStale, domain.InboundMismatch, domain.InboundUnmapped:
		return true
	}
	return false
}

func sameOptional(a, b *string) bool { return a != nil && b != nil && *a == *b }

// Claim inserts v unless a claimed row with its connection, event key and
// provider status exists, like the partial unique index.
func (s *InboundWebhooks) Claim(_ context.Context, v *domain.InboundWebhook) (bool, error) {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, cur := range w.inbound {
		if cur.ConnectionID == v.ConnectionID && sameOptional(cur.EventKey, v.EventKey) &&
			sameOptional(cur.ProviderStatus, v.ProviderStatus) && claimed(cur.Result) {
			return false, nil
		}
	}
	w.insertInbound(v)
	return true, nil
}

// Create inserts v.
func (s *InboundWebhooks) Create(_ context.Context, v *domain.InboundWebhook) error {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.InboundCreateErr != nil {
		return w.InboundCreateErr
	}
	w.insertInbound(v)
	return nil
}

// insertInbound stores v; the caller holds w.mu.
func (w *Webhooks) insertInbound(v *domain.InboundWebhook) {
	v.ID = w.id()
	v.ReceivedAt = w.now()
	stored := *v
	stored.RawBody = slices.Clone(v.RawBody)
	w.inbound[v.ID] = stored
}

// UpdateOutcome writes the result fields of v.
func (s *InboundWebhooks) UpdateOutcome(_ context.Context, v *domain.InboundWebhook) error {
	w := s.w
	w.mu.Lock()
	defer w.mu.Unlock()
	cur, ok := w.inbound[v.ID]
	if !ok {
		return domain.ErrNotFound
	}
	cur.Result, cur.ResultDetail, cur.PaymentID, cur.EventID = v.Result, v.ResultDetail, v.PaymentID, v.EventID
	w.inbound[v.ID] = cur
	return nil
}

// fillInbound sets the joined fields of v; the caller holds w.mu.
func (w *Webhooks) fillInbound(v *domain.InboundWebhook) {
	if w.conns != nil {
		for _, c := range w.conns.All() {
			if c.ID == v.ConnectionID {
				v.Connection = c.Summary()
			}
		}
	}
	v.PaymentPublicID, v.Event = "", nil
	if v.PaymentID != nil && w.payments != nil {
		for _, p := range w.payments.All() {
			if p.ID == *v.PaymentID {
				v.PaymentPublicID = p.PublicID
			}
		}
	}
	if v.EventID != nil {
		if e, ok := w.events[*v.EventID]; ok {
			v.Event = &domain.EventRef{PublicID: e.PublicID, Type: e.Type, IsTest: e.IsTest}
		}
	}
}

// List returns one page of inbound webhooks without their body.
func (s *InboundWebhooks) List(_ context.Context, f domain.InboundFilter, p domain.Page) (domain.PageResult[domain.InboundWebhook], error) {
	var items []domain.InboundWebhook
	for _, v := range s.w.AllInbound() {
		switch {
		case f.ConnectionPublicID != nil && v.Connection.PublicID != *f.ConnectionPublicID,
			f.Result != nil && v.Result != *f.Result,
			f.Environment != nil && v.Connection.Environment != *f.Environment,
			f.From != nil && v.ReceivedAt.Before(*f.From),
			f.To != nil && v.ReceivedAt.After(*f.To):
			continue
		}
		v.RawBody = nil
		items = append(items, v)
	}
	return page(items, p, func(v domain.InboundWebhook) domain.Cursor { return domain.Cursor{Time: v.ReceivedAt, ID: v.PublicID} }), nil
}

// GetByPublicID loads an inbound webhook with its body.
func (s *InboundWebhooks) GetByPublicID(_ context.Context, publicID string) (*domain.InboundWebhook, error) {
	for _, v := range s.w.AllInbound() {
		if v.PublicID == publicID {
			return &v, nil
		}
	}
	return nil, domain.ErrNotFound
}
