/*
----------------------------------------
@Create 2023/11/17-16:01
@Author lpc<lengpucheng@qq.com>
@Program Geassgo
@Describe claim_execute
----------------------------------------
@Version 1.0 2023/11/17
@Memo create this file
*/

package helper

import (
	"fmt"
	"github.com/lengpucheng/Geassgo/pkg/coderender"
	"github.com/lengpucheng/Geassgo/pkg/geasserr"
	"github.com/lengpucheng/Geassgo/pkg/profess/contract"
	"github.com/lengpucheng/Geassgo/pkg/profess/geass"
	"gopkg.in/yaml.v3"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func init() {
	geass.RegisterGeass(Claim, &helperClaim{})
}

const Claim = "_CLAIM_HELPER_"

type helperClaim struct{}

func (c *helperClaim) Execute(ctx contract.Context, val any) error {
	claim := val.(*contract.Claim)
	// 判断是否被筛选
	if !claim.IsSelect(ctx) {
		return nil
	}

	if ctx.GetItemIndex() < 0 {
		slog.Info("********Task:", "name", claim.Name)
	}
	if claim.Loop == nil {
		return c.executeClaim(ctx, claim)
	}
	var retry int
	// 当 loop 不为nil 时 加入循环
	for {
		// 判断是否超时
		retry++
		if retry > claim.Loop.Retries {
			return c.withError(fmt.Errorf("loop retry timeout---> retry %d ", retry), ctx, claim)
		}
		// 执行本次claim
		err := c.executeClaim(ctx, claim)
		slog.Info("loop this task,please wait", "retry", retry)
		// 判断是否达成条件
		if str, e := geass.RenderStr(ctx, claim.Loop.Until); e == nil && strings.ToLower(str) == "true" {
			// 满足条件后返回执行的可能错误
			return err
		}

		// 延迟等待
		time.Sleep(time.Duration(claim.Loop.Delay) * time.Second)
	}
}

// 执行正常的claim内容
// ctx 上下文对象
// claim 任务清单
func (c *helperClaim) executeClaim(ctx contract.Context, claim *contract.Claim) error {
	if !claim.IsWhen(ctx.GetVariable()) {
		slog.Info("skipping")
		return nil
	}
	var err error

	// 执行
	if claim.WithItem != nil {
		err = c.withItem(ctx, claim)
	} else if claim.Include != "" {
		err = c.withInclude(ctx, claim)
	} else if claim.Roles != nil {
		err = c.withRoles(ctx, claim)
	} else if claim.Tasks != nil {
		err = c.withTasks(ctx, claim)
	} else {
		err = c.withGeass(ctx, claim)
	}

	// 错误处理
	err = c.withError(err, ctx, claim)
	// 注册变量
	if claim.Register != "" {
		ctx.GetVariable().Register[claim.Register] = ctx.GetStdout()
	}
	return err
}

func (c *helperClaim) OverallRender() bool {
	return false
}

func (c *helperClaim) OverloadRender() (bool, any) {
	return false, nil
}

func (c *helperClaim) withGeass(ctx contract.Context, claim *contract.Claim) error {
	claim.Task.Mod = claim.Mod // 转移MOD
	if err := geass.Execute(ctx, geass.Task, claim.Task); err != nil {
		return err
	}
	slog.Info("Ok")
	return nil
}

// 对嵌套claims的执行
func (c *helperClaim) withTasks(ctx contract.Context, claim *contract.Claim) error {
	slog.Info(">>>>>>>>", "tasks", len(claim.Tasks))
	for _, subTask := range claim.Tasks {
		if err := geass.Execute(ctx.SubContext(ctx), Claim, &subTask); err != nil {
			return err
		}
	}
	return nil
}

// 对导入claims
func (c *helperClaim) withInclude(ctx contract.Context, claim *contract.Claim) error {
	include, err := geass.RenderStr(ctx, claim.Include)
	slog.Info(">>>>>>>>", "include", include)
	if err != nil {
		return err
	}
	return LoadAndExecute4File(ctx, include)
}

// 对 roles的执行
func (c *helperClaim) withRoles(ctx contract.Context, claim *contract.Claim) error {
	slog.Info(">>>>>>>>", "roles", len(claim.Roles))
	for _, role := range claim.Roles {
		role, err := geass.RenderStr(ctx, role)
		slog.Info(">>>>>>>>", "role", role, "name", claim.Name)
		if err != nil {
			return err
		}
		// 拼接roles目录
		rolePath := filepath.Join(filepath.Dir(ctx.GetLocation()), "roles", role) + "/"
		if err := geass.Execute(ctx.SubContext(geass.NewRuntime(rolePath, rolePath, -1, nil)), Roles, nil); err != nil {
			return err
		}
	}
	return nil
}

// 对withItem的执行
func (c *helperClaim) withItem(ctx contract.Context, claim *contract.Claim) error {
	slog.Info(">>>>>>>>", "withItems", len(claim.WithItem))
	for index, item := range claim.WithItem {
		slog.Info(">>>>>>>>", "item", index, "name", claim.Name)
		rItem, err := geass.RenderStr(ctx, item)
		if err != nil {
			return err
		}
		var itemClaim = *claim
		itemClaim.WithItem = nil
		if err = geass.Execute(ctx.SubContext(geass.NewRuntime(ctx.GetLocation(), ctx.GetRolePath(), index, rItem)), Claim, &itemClaim); err != nil {
			return err
		}
	}
	return nil
}

// 错误处理
func (c *helperClaim) withError(err error, ctx contract.Context, claim *contract.Claim) error {
	ctx.GetVariable().System.LastErr = err == nil
	ctx.GetVariable().System.LastIgnore = err != nil && claim.IgnoreError
	if err != nil {
		if claim.IgnoreError {
			slog.Warn("Ignore.....", "error", err.Error(), "stderr", ctx.GetStderr())
			return nil
		}
		slog.Error("Error.....", "error", err.Error(), "stderr", ctx.GetStderr())
		return err
	}
	return nil
}

// LoadAndExecute4File 从文件加载并执行Claim
func LoadAndExecute4File(ctx contract.Context, path string) error {
	absPath := coderender.AbsPath(filepath.Dir(ctx.GetLocation()), path)
	file, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}
	node := yaml.Node{}
	if err := yaml.Unmarshal(file, &node); err != nil {
		return err
	}
	if len(node.Content) < 1 {
		return geasserr.ClaimYamlDecodeFail.New()
	}
	switch node.Content[0].Kind {
	case yaml.SequenceNode:
		inClaim := new([]contract.Claim)
		if err = node.Decode(inClaim); err != nil {
			return err
		}
		for _, inClaimItem := range *inClaim {
			if err = geass.Execute(ctx.SubContext(geass.NewRuntime(absPath, ctx.GetRolePath(), ctx.GetItemIndex(), ctx.GetItem())), Claim, &inClaimItem); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		fallthrough
	default:
		inClaim := new(contract.Claim)
		if err = yaml.Unmarshal(file, inClaim); err != nil {
			return err
		}
		return geass.Execute(ctx.SubContext(geass.NewRuntime(absPath, ctx.GetRolePath(), ctx.GetItemIndex(), ctx.GetItem())), Claim, inClaim)
	}
	return nil
}
