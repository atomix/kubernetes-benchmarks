// Copyright 2019-present Open Networking Foundation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package _map

import (
	"context"
	"errors"
	"fmt"
	atomix "github.com/atomix/go-client/pkg/client"
	"github.com/onosproject/helmit/pkg/helm"
	"github.com/onosproject/helmit/pkg/input"
	"github.com/onosproject/helmit/pkg/kubernetes"
	"time"

	"github.com/atomix/go-client/pkg/client/map"
	"github.com/onosproject/helmit/pkg/benchmark"
)

// MapBenchmarkSuite :: benchmark
type MapBenchmarkSuite struct {
	benchmark.Suite
	key     input.Source
	value   input.Source
	_map    _map.Map
	watchCh chan *_map.Event
}

// SetupSuite :: benchmark
func (s *MapBenchmarkSuite) SetupSuite(c *benchmark.Context) error {
	err := helm.Chart("atomix-controller").
		Release("atomix-controller").
		Set("scope", "Namespace").
		Install(true)
	if err != nil {
		return err
	}

	err = helm.Chart("atomix-database").
		Release("atomix-database").
		Set("clusters", 3).
		Set("partitions", 10).
		Set("backend.replicas", 3).
		Set("backend.image", "atomix/local-replica:latest").
		Install(true)
	if err != nil {
		return err
	}
	return nil
}

// SetupWorker :: benchmark
func (s *MapBenchmarkSuite) SetupWorker(c *benchmark.Context) error {
	s.key = input.RandomChoice(
		input.SetOf(
			input.RandomString(c.GetArg("key-length").Int(8)),
			c.GetArg("key-count").Int(1000)))
	s.value = input.RandomChoice(
		input.SetOf(
			input.RandomBytes(c.GetArg("value-length").Int(128)),
			c.GetArg("value-count").Int(1)))
	return nil
}

func (s *MapBenchmarkSuite) getController() (string, error) {
	client, err := kubernetes.NewForRelease(helm.Release("atomix-controller"))
	if err != nil {
		return "", err
	}
	services, err := client.CoreV1().Services().List()
	if err != nil {
		return "", err
	}
	if len(services) == 0 {
		return "", nil
	}
	service := services[0]
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d", service.Name, service.Namespace, service.Ports()[0].Port), nil
}

// SetupBenchmark :: benchmark
func (s *MapBenchmarkSuite) SetupBenchmark(c *benchmark.Context) error {
	address, err := s.getController()
	if err != nil {
		return err
	}

	client, err := atomix.New(address)
	if err != nil {
		return err
	}

	database, err := client.GetDatabase(context.Background(), "atomix-database")
	if err != nil {
		return err
	}

	_map, err := database.GetMap(context.Background(), c.Name)
	if err != nil {
		return err
	}
	s._map = _map
	return nil
}

// TearDownBenchmark :: benchmark
func (s *MapBenchmarkSuite) TearDownBenchmark(c *benchmark.Context) {
	s._map.Close(context.Background())
}

// BenchmarkMapPut :: benchmark
func (s *MapBenchmarkSuite) BenchmarkMapPut(b *benchmark.Benchmark) error {
	_, err := s._map.Put(context.Background(), s.key.Next().String(), s.value.Next().Bytes())
	return err
}

// BenchmarkMapGet :: benchmark
func (s *MapBenchmarkSuite) BenchmarkMapGet(b *benchmark.Benchmark) error {
	_, err := s._map.Get(context.Background(), s.key.Next().String())
	return err
}

// SetupBenchmarkMapEvent sets up the map event benchmark
func (s *MapBenchmarkSuite) SetupBenchmarkMapEvent(c *benchmark.Context) {
	watchCh := make(chan *_map.Event)
	if err := s._map.Watch(context.Background(), watchCh); err != nil {
		panic(err)
	}
	s.watchCh = watchCh
}

// TearDownBenchmarkMapEvent tears down the map event benchmark
func (s *MapBenchmarkSuite) TearDownBenchmarkMapEvent(c *benchmark.Context) {
	s.watchCh = nil
}

// BenchmarkMapEvent :: benchmark
func (s *MapBenchmarkSuite) BenchmarkMapEvent(b *benchmark.Benchmark) error {
	_, err := s._map.Put(context.Background(), s.key.Next().String(), s.value.Next().Bytes())
	select {
	case <-s.watchCh:
		return err
	case <-time.After(10 * time.Second):
		return errors.New("event timeout")
	}
}

// SetupBenchmarkMapEntries sets up the map entries benchmark
func (s *MapBenchmarkSuite) SetupBenchmarkMapEntries(c *benchmark.Context) error {
	for i := 0; i < c.GetArg("key-count").Int(1000); i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := s._map.Put(ctx, s.key.Next().String(), s.value.Next().Bytes())
		if err != nil {
			return err
		}
		cancel()
	}
	return nil
}

// BenchmarkMapEntries :: benchmark
func (s *MapBenchmarkSuite) BenchmarkMapEntries(b *benchmark.Benchmark) error {
	ch := make(chan *_map.Entry)
	err := s._map.Entries(context.Background(), ch)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			return errors.New("event timeout")
		}
	}
}
