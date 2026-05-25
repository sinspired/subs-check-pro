// Package app: errors.go — 应用级别可传播错误哨兵值
package app

import "errors"

// ErrFirstRun 首次运行错误哨兵值。
//
// 当 config.yaml 不存在时，Initialize() 会创建默认配置文件并返回此错误。
// 调用方（CLI / GUI）应根据自身模式做不同处理：
//   - CLI: 打印提示信息后 os.Exit(0)
//   - GUI: 设置环境变量通知前端登录窗口展示引导提示，不退出进程
//
// 由于 Initialize() 内部用 fmt.Errorf("%w", ...) 包装，
// 调用方需使用 errors.Is(err, app.ErrFirstRun) 判断。
var ErrFirstRun = errors.New("first-run: config.yaml already created, please edit and restart")
