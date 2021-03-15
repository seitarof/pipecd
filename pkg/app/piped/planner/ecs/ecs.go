// Copyright 2020 The PipeCD Authors.
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

package ecs

import (
	"context"
	"fmt"
	"io/ioutil"
	"time"

	"go.uber.org/zap"

	provider "github.com/pipe-cd/pipe/pkg/app/piped/cloudprovider/ecs"
	"github.com/pipe-cd/pipe/pkg/app/piped/planner"
	"github.com/pipe-cd/pipe/pkg/model"
)

// Planner plans the deployment pipeline for ECS application.
type Planner struct {
}

type registerer interface {
	Register(k model.ApplicationKind, p planner.Planner) error
}

// Register registers this planner into the given registerer.
func Register(r registerer) {
	r.Register(model.ApplicationKind_ECS, &Planner{})
}

// Plan decides which pipeline should be used for the given input.
func (p *Planner) Plan(ctx context.Context, in planner.Input) (out planner.Output, err error) {
	ds, err := in.TargetDSP.Get(ctx, ioutil.Discard)
	if err != nil {
		err = fmt.Errorf("error while preparing deploy source data (%v)", err)
		return
	}

	cfg := ds.DeploymentConfig.ECSDeploymentSpec
	if cfg == nil {
		err = fmt.Errorf("missing ECSDeploymentSpec in deployment configuration")
		return
	}

	// Determine application version from the task definition
	if version, err := determineVersion(ds.AppDir, cfg.Input.TaskDefinitionFile); err == nil {
		out.Version = version
	} else {
		out.Version = "unknown"
		in.Logger.Warn("unable to determine target version", zap.Error(err))
	}

	// If the deployment was triggered by forcing via web UI,
	// we rely on the user's decision.
	switch in.Deployment.Trigger.SyncStrategy {
	case model.SyncStrategy_QUICK_SYNC:
		out.Stages = buildQuickSyncPipeline(cfg.Input.AutoRollback, time.Now())
		out.Summary = fmt.Sprintf("Quick sync to deploy image %s and configure all traffic to it (forced via web)", out.Version)
		return
	}

	// If this is the first time to deploy this application or it was unable to retrieve last successful commit,
	// we perform the quick sync strategy.
	if in.MostRecentSuccessfulCommitHash == "" {
		out.Stages = buildQuickSyncPipeline(cfg.Input.AutoRollback, time.Now())
		out.Summary = fmt.Sprintf("Quick sync to deploy image %s and configure all traffic to it (it seems this is the first deployment)", out.Version)
		return
	}

	// When no pipeline was configured, perform the quick sync.
	if cfg.Pipeline == nil || len(cfg.Pipeline.Stages) == 0 {
		out.Stages = buildQuickSyncPipeline(cfg.Input.AutoRollback, time.Now())
		out.Summary = fmt.Sprintf("Quick sync to deploy image %s and configure all traffic to it (pipeline was not configured)", out.Version)
		return
	}

	out.Stages = buildQuickSyncPipeline(cfg.Input.AutoRollback, time.Now())
	out.Summary = fmt.Sprintf("Quick sync to deploy image %s and configure all traffic to it", out.Version)
	return
}

func determineVersion(appDir, serviceDefinitonFile string) (string, error) {
	taskDefinition, err := provider.LoadTaskDefinition(appDir, serviceDefinitonFile)
	if err != nil {
		return "", err
	}

	return provider.FindImageTag(taskDefinition)
}