package assembly

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type fakeDB struct {
	storage.Database
	ports.Resource
}
type fakeRedis struct{ ports.Resource }

func (fakeRedis) Do(context.Context, string, ...any) (any, error) {
	return nil, errors.New("injected unavailable")
}

type closeSource struct {
	Source
	ports.Resource
}

func (s closeSource) Close(ctx context.Context) error {
	return errors.Join(s.Source.Close(ctx), s.Resource.Close(ctx))
}

type closeMail struct {
	Mail
	ports.Resource
}

func (m closeMail) Close(ctx context.Context) error {
	return errors.Join(m.Mail.Close(ctx), m.Resource.Close(ctx))
}
func fixture(t *testing.T) (config.Runtime, map[string]string) {
	t.Helper()
	v := testkit.RuntimeValues(t.TempDir())
	c, e := config.LoadRuntime(config.Worker, func(k string) (string, bool) { s, ok := v[k]; return s, ok })
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{c.LogDirectory, c.StagingDirectory, c.TargetDirectory, c.SpoolDirectory} {
		if e = os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	return c, v
}
func trackedFactories(trace *[]string, fail string) Factories {
	f := RealFactories()
	mark := func(name string) (ports.Resource, error) {
		*trace = append(*trace, "open:"+name)
		var e error
		if fail == name {
			e = errors.New("synthetic-secret-provider-error")
		}
		return testkit.CloseFunc(func(context.Context) error { *trace = append(*trace, "close:"+name); return nil }), e
	}
	f.Postgres = func(context.Context, config.Runtime) (Database, error) {
		r, e := mark("postgres")
		return &fakeDB{Resource: r}, e
	}
	f.Redis = func(context.Context, config.Runtime) (Redis, error) { r, e := mark("redis"); return &fakeRedis{r}, e }
	files := f.Files
	f.Files = func(ctx context.Context, c config.Runtime) (*FileSet, error) {
		r, e := mark("files")
		v, x := files(ctx, c)
		if v != nil {
			close := v.close
			v.close = func(ctx context.Context) error { return errors.Join(close(ctx), r.Close(ctx)) }
		}
		return v, errors.Join(e, x)
	}
	source := f.Source
	f.Source = func(ctx context.Context, c config.Runtime) (Source, error) {
		r, e := mark("source")
		v, x := source(ctx, c)
		return closeSource{v, r}, errors.Join(e, x)
	}
	mail := f.Mail
	f.Mail = func(ctx context.Context, c config.Runtime) (Mail, error) {
		r, e := mark("mail")
		v, x := mail(ctx, c)
		return closeMail{v, r}, errors.Join(e, x)
	}
	return f
}
func TestT15_09PartialFactoryFailureReleasesEverythingInReverse(t *testing.T) {
	names := []string{"postgres", "redis", "files", "source", "mail"}
	for i, name := range names {
		t.Run(name, func(t *testing.T) {
			c, _ := fixture(t)
			var trace []string
			r, results, e := Open(t.Context(), c, trackedFactories(&trace, name))
			if e == nil || r != nil || strings.Contains(e.Error(), "synthetic-secret") {
				t.Fatal(r, e)
			}
			var expected []string
			for _, n := range names[:i+1] {
				expected = append(expected, "open:"+n)
			}
			for j := i; j >= 0; j-- {
				expected = append(expected, "close:"+names[j])
			}
			if !reflect.DeepEqual(trace, expected) || len(results) != i+1 {
				t.Fatal(trace, results)
			}
			for _, v := range results {
				if v.State != "closed" {
					t.Fatal(results)
				}
			}
		})
	}
}
func TestT15_09InvalidConfigOrMissingFactoryCreatesNothing(t *testing.T) {
	for _, name := range []string{"postgres", "redis", "files", "source", "mail", "configuration"} {
		t.Run(name, func(t *testing.T) {
			c, _ := fixture(t)
			var trace []string
			f := trackedFactories(&trace, "")
			switch name {
			case "postgres":
				f.Postgres = nil
			case "redis":
				f.Redis = nil
			case "files":
				f.Files = nil
			case "source":
				f.Source = nil
			case "mail":
				f.Mail = nil
			default:
				c.SourceURL = ""
			}
			r, _, e := Open(t.Context(), c, f)
			if e == nil || r != nil || len(trace) != 0 {
				t.Fatal(trace, e)
			}
		})
	}
}
func TestT15_09SuccessfulAssemblyHasNoFallbackAndClosesOnce(t *testing.T) {
	c, _ := fixture(t)
	var trace []string
	r, _, e := Open(t.Context(), c, trackedFactories(&trace, ""))
	if e != nil {
		t.Fatal(e)
	}
	if r.Control.Database != r.Database || r.Logs == nil || r.Sanitizer == nil || r.Active.Client != r.Redis || r.Cache.Client != r.Redis {
		t.Fatal("components not wired")
	}
	if _, e = r.Cache.Get(t.Context(), "synthetic-key"); e == nil {
		t.Fatal("Redis error hidden by fallback")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if e = r.Close(ctx); e != nil {
		t.Fatal(e)
	}
	if e = r.Close(ctx); e != nil {
		t.Fatal(e)
	}
	if len(trace) != 10 || trace[5] != "close:mail" || trace[9] != "close:postgres" {
		t.Fatal(trace)
	}
}

func TestWorkerWithoutMailSkipsMailDependency(t *testing.T) {
	for _, missingFactory := range []bool{false, true} {
		t.Run(fmt.Sprint(missingFactory), func(t *testing.T) {
			c, _ := fixture(t)
			c.MailAddress, c.MailTLS, c.MailFrom, c.MailTo, c.MailUser, c.MailPassword = "", "", "", "", "", ""
			var trace []string
			f := trackedFactories(&trace, "mail")
			if missingFactory {
				f.Mail = nil
			}
			r, _, err := Open(t.Context(), c, f)
			if err != nil {
				t.Fatal(err)
			}
			if r.Mail != nil || r.Source == nil || r.Logs == nil {
				t.Fatal("unexpected optional mail wiring")
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := r.Close(ctx); err != nil {
				t.Fatal(err)
			}
			want := []string{"open:postgres", "open:redis", "open:files", "open:source", "close:source", "close:files", "close:redis", "close:postgres"}
			if !reflect.DeepEqual(trace, want) {
				t.Fatal(trace)
			}
		})
	}
}
func TestT15_09MissingPersistentDirectoryFailsAndReleasesEarlierResources(t *testing.T) {
	c, _ := fixture(t)
	c.TargetDirectory += "-missing"
	var trace []string
	r, results, e := Open(t.Context(), c, trackedFactories(&trace, ""))
	if e == nil || r != nil || len(results) != 3 || trace[len(trace)-1] != "close:postgres" {
		t.Fatal(trace, results, e)
	}
}

func TestT15_09FileAliasRejectedAndOverQuotaRetainedWithoutStartupFailure(t *testing.T) {
	for _, scenario := range []string{"alias", "quota"} {
		t.Run(scenario, func(t *testing.T) {
			c, _ := fixture(t)
			marker := filepath.Join(c.LogDirectory, "retained.event")
			if e := os.WriteFile(marker, []byte(strings.Repeat("x", 2048)), 0600); e != nil {
				t.Fatal(e)
			}
			if scenario == "alias" {
				alias := filepath.Join(filepath.Dir(c.LogDirectory), "log-alias")
				if e := os.Symlink(c.LogDirectory, alias); e != nil {
					t.Fatal(e)
				}
				c.TargetDirectory = alias
			} else {
				c.LogBytes = 1024
			}
			var trace []string
			r, _, e := Open(t.Context(), c, trackedFactories(&trace, ""))
			if scenario == "alias" && (e == nil || r != nil) {
				t.Fatal("unsafe file setup accepted")
			}
			if scenario == "quota" {
				if e != nil || r == nil {
					t.Fatal("full log quota blocked business startup")
				}
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				if r.Close(ctx) != nil {
					t.Fatal("cleanup")
				}
			}
			if b, e := os.ReadFile(marker); e != nil || len(b) != 2048 {
				t.Fatal("existing data removed", e)
			}
		})
	}
}
