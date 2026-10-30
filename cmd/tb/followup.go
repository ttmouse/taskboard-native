package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

func cmdFollowup(args []string) int {
	fs := flag.NewFlagSet("followup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "", "service address")
	agent := fs.String("agent", "", "agent identity")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		fmt.Fprintln(os.Stderr, "tb followup:", err)
		return exitUsage
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "tb followup: usage: tb followup <taskID> <commentID>")
		return exitUsage
	}
	c, err := ensureClient(*addr, *agent)
	if err != nil {
		return fail(err)
	}
	path := "/api/tasks/" + url.PathEscape(fs.Arg(0)) + "/comments/" + url.PathEscape(fs.Arg(1)) + "/continue"
	var accepted map[string]string
	if err := c.send(http.MethodPost, path, nil, &accepted, http.StatusAccepted); err != nil {
		return fail(err)
	}
	fmt.Println("已接收评论跟进请求；执行进展请查看原卡。")
	return exitOK
}
