package timeline_reader_fd_leak_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"runtime/debug"
	"syscall"
	"testing"

	"paperqual/internal/api"
	"paperqual/internal/application"
	"paperqual/internal/domain"
	"paperqual/internal/store"
)

func TestRepeatedTimelineReadsReleaseFileDescriptors(t *testing.T) {
	repo, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewService(repo)
	_, err = service.Create(
		application.CommandMeta{RequestID: "create-fd-case", ActorID: "operator"},
		application.CreateBatch{
			BatchID:    "fd-case",
			Title:      "时间线描述符所有权复现",
			OperatorID: "operator",
			ReviewerID: "reviewer",
			Standards: domain.Standards{
				TargetSurfacePHMin:    7,
				TargetSurfacePHMax:    9,
				MinAlkalineReservePct: 1,
				MaxColorDeltaE:        3,
				SampleRatio:           1,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	openDescriptors, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("读取当前描述符数量: %v", err)
	}
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &original); err != nil {
		t.Fatalf("读取 RLIMIT_NOFILE: %v", err)
	}
	limited := original
	limited.Cur = uint64(len(openDescriptors) + 16)
	if limited.Cur > original.Cur {
		t.Fatalf("当前 RLIMIT_NOFILE 太低，无法建立复现边界: cur=%d open=%d", original.Cur, len(openDescriptors))
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limited); err != nil {
		t.Fatalf("设置 RLIMIT_NOFILE: %v", err)
	}
	defer syscall.Setrlimit(syscall.RLIMIT_NOFILE, &original)
	previousGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previousGC)

	handler := api.NewServer(service)
	for i := 0; i < 32; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/batches/fd-case", nil)
		req.Header.Set("Accept", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("第 %d 次读取返回 %d，事件日志描述符未在请求结束时释放: %s", i+1, response.Code, response.Body.String())
		}
	}
}
