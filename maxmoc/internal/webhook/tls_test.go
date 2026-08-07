package webhook

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"maxmock/internal/testcert"
	"maxmock/internal/wire"
)

// tlsStand поднимает стенд на HTTPS с самоподписанным сертификатом — так
// выглядит типовой стенд КЦ в закрытом контуре — и возвращает счётчик
// доставок вместе с путём к файлу его CA.
func tlsStand(t *testing.T) (*httptest.Server, *atomic.Int32, string) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	caFile := filepath.Join(t.TempDir(), "stand-ca.pem")
	if err := testcert.WritePEM(caFile, srv.Certificate().Raw); err != nil {
		t.Fatal(err)
	}
	return srv, &hits, caFile
}

func TestDeliversOverTLSWithCAFile(t *testing.T) {
	stand, hits, caFile := tlsStand(t)

	cfg := fastCfg()
	cfg.CAFile = caFile
	f := newFixture(t, cfg)

	if _, err := f.store.AddSubscription(f.bot.ID, stand.URL, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.disp.Deliver(f.bot.ID, f.chat, wire.UpdateMessageRemoved, removedUpdate(f.chat, "mid.1")); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	if n := hits.Load(); n != 1 {
		t.Fatalf("доставок на https-стенд: %d, ожидалась 1", n)
	}
	deliveries, _ := f.store.ListDeliveries(f.bot.ID, 10)
	if len(deliveries) != 1 || deliveries[0].Status != 200 {
		t.Errorf("журнал доставок: %+v", deliveries)
	}
}

// Без CA доставка обязана падать — и падать так, чтобы причина была видна во
// вкладке «Доставки», а не только в логе процесса.
func TestDeliveryOverTLSFailsWithoutCA(t *testing.T) {
	stand, hits, _ := tlsStand(t)

	f := newFixture(t, fastCfg())
	if _, err := f.store.AddSubscription(f.bot.ID, stand.URL, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.disp.Deliver(f.bot.ID, f.chat, wire.UpdateMessageRemoved, removedUpdate(f.chat, "mid.1")); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	if n := hits.Load(); n != 0 {
		t.Fatalf("событие ушло на непроверенный стенд %d раз", n)
	}
	deliveries, _ := f.store.ListDeliveries(f.bot.ID, 10)
	if len(deliveries) == 0 {
		t.Fatal("отказ доставки не попал в журнал")
	}
	if !strings.Contains(deliveries[0].Error, "x509") && !strings.Contains(deliveries[0].Error, "certificate") {
		t.Errorf("в журнале нет причины отказа TLS: %q", deliveries[0].Error)
	}
}

func TestDeliversOverTLSWithInsecureSkipVerify(t *testing.T) {
	stand, hits, _ := tlsStand(t)

	cfg := fastCfg()
	cfg.InsecureSkipVerify = true
	f := newFixture(t, cfg)

	if _, err := f.store.AddSubscription(f.bot.ID, stand.URL, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.disp.Deliver(f.bot.ID, f.chat, wire.UpdateMessageRemoved, removedUpdate(f.chat, "mid.1")); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	if n := hits.Load(); n != 1 {
		t.Errorf("с отключённой проверкой доставок: %d, ожидалась 1", n)
	}
}

// Нечитаемый CA-файл должен валить создание диспетчера, то есть старт
// сервиса: иначе расхождение проявится как «события не приходят».
func TestNewRejectsBrokenCAFile(t *testing.T) {
	cfg := fastCfg()
	cfg.CAFile = filepath.Join(t.TempDir(), "нет.pem")
	if _, err := New(nil, nil, nil, cfg); err == nil {
		t.Fatal("диспетчер создан с нечитаемым CA-файлом")
	}
}
