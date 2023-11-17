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
	"github.com/lengpucheng/Geassgo/pkg/coderender"
	"github.com/lengpucheng/Geassgo/pkg/geasserr"
	"github.com/lengpucheng/Geassgo/pkg/profess/contract"
	"github.com/lengpucheng/Geassgo/pkg/profess/geass"
	"gopkg.in/yaml.v3"
	"log/slog"
	"os"
	"path/filepath"
)

func init() {
	geass.RegisterGeass(Claim, &claimHelper{})
}

const Claim = "_CLAIM_HELPER_"

type claimHelper struct{}

func (c *claimHelper) Execute(ctx contract.Context, val any) error {
	claim := val.(*contract.Claim)
	slog.Info("********Task:", "name", claim.Name)
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
		claim.Task.Mod = claim.Mod // 转移MOD
		err = geass.Execute(ctx, geass.Task, claim.Task)
	}

	// 错误处理
	if err = c.withError(err, ctx, claim); err != nil {
		return err
	}
	// 注册变量
	if claim.Register != "" {
		ctx.GetVariable().Register[claim.Register] = ctx.GetStdout()
	}
	return err
}

func (c *claimHelper) OverallRender() bool {
	return false
}

func (c *claimHelper) OverloadRender() (bool, any) {
	return false, nil
}

// 对嵌套claims的执行
func (c *claimHelper) withTasks(ctx contract.Context, claim *contract.Claim) error {
	for _, subTask := range claim.Tasks {
		return geass.Execute(ctx.SubContext(ctx.GetItem(), ctx.GetItemIndex()), Claim, &subTask)
	}
	return nil
}

// 对导入claims
func (c *claimHelper) withInclude(ctx contract.Context, claim *contract.Claim) error {
	return LoadAndExecute4File(ctx, claim.Include)
}

// 对 roles的执行
func (c *claimHelper) withRoles(ctx contract.Context, claim *contract.Claim) error {
	for _, role := range claim.Roles {
		if err := LoadAndExecute4File(ctx, filepath.Join(role, "main.yaml")); err != nil {
			return err
		}
	}
	return nil
}

// 对withItem的执行
func (c *claimHelper) withItem(ctx contract.Context, claim *contract.Claim) error {
	for index, item := range claim.WithItem {
		rItem, err := geass.RenderStr(ctx, item)
		if err != nil {
			return err
		}
		var itemClaim = *claim
		itemClaim.WithItem = nil
		if err = geass.Execute(ctx.SubContext(rItem, index), Claim, &itemClaim); err != nil {
			return err
		}
	}
	return nil
}

// 错误处理
func (c *claimHelper) withError(err error, ctx contract.Context, claim *contract.Claim) error {
	if err != nil {
		if claim.IgnoreError {
			slog.Warn("Ignore.....", "error", err.Error(), "stderr", ctx.GetStderr())
			return nil
		}
		slog.Error("Error.....", "error", err.Error(), "stderr", ctx.GetStderr())
		return err
	}
	slog.Info("Ok")
	return nil
}

// LoadAndExecute4File 从文件加载并执行Claim
func LoadAndExecute4File(ctx contract.Context, path string) error {
	file, err := os.ReadFile(coderender.AbsPath(ctx.GenLocation(), path))
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
			if err = geass.Execute(ctx.SubContext(ctx.GetItem(), ctx.GetItemIndex()), Claim, &inClaimItem); err != nil {
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
		return geass.Execute(ctx, Claim, inClaim)
	}
	return nil
}