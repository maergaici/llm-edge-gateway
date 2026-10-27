package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// ★ 修正 1：Content-Type
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			// ★ 修正 2：unsupported
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		fmt.Println("收到客户端请求，开始无限流式输出...")
		words := []string{"你", "好", "我", "是", "C", "a", "r", "a", "t", "!", "!", "!"}

		i := 0
		for {
			select {
			case <-r.Context().Done():
				fmt.Println("客户端断开连接，停止输出")
				return // 退出循环，释放资源
			default:
				word := words[i%len(words)]
				fmt.Fprintf(w, "data:{\"text\":\"%s\"}\n\n", word)
				flusher.Flush()
				time.Sleep(100 * time.Millisecond)
				i++
			}
		}
	})

	fmt.Println("Mock LLM 后端启动在 :9003")
	http.ListenAndServe(":9003", nil)
}
