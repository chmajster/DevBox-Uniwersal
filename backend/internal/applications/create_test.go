package applications

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestCreateDifferentNamesWithSameSlug(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan Detail, 2)
	failures := make(chan error, 2)
	for _, name := range []string{"My App", "My-App"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			app, err := f.service.Create(ctx, CreateInput{Name: name, SourceType: SourceEmpty}, nil)
			if err != nil {
				failures <- err
				return
			}
			results <- app
		}(name)
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	slugs := map[string]bool{}
	for app := range results {
		if slugs[app.Slug] {
			t.Fatalf("duplicate slug: %s", app.Slug)
		}
		slugs[app.Slug] = true
		if _, err := f.repo.Source(ctx, app.ID); err != nil {
			t.Fatal(err)
		}
	}
	if len(slugs) != 2 || !slugs["my-app"] {
		t.Fatalf("slugs = %v", slugs)
	}
	apps, err := f.repo.List(ctx)
	if err != nil || len(apps) != 2 {
		t.Fatalf("apps = %v, error = %v", apps, err)
	}
}

func TestCreateDuplicateNameReportsActionableConflict(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for i, name := range []string{"Example", " example "} {
		_, err := f.service.Create(ctx, CreateInput{Name: name, SourceType: SourceEmpty}, nil)
		if i == 0 && err != nil {
			t.Fatal(err)
		}
		if i == 1 && (!errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "with this name already exists")) {
			t.Fatalf("duplicate error = %v", err)
		}
	}
	apps, err := f.repo.List(ctx)
	if err != nil || len(apps) != 1 {
		t.Fatalf("apps = %v, error = %v", apps, err)
	}
}
