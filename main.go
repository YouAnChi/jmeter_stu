package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/panjf2000/ants/v2"
)

// 全局变量定义
var (
	pool        *ants.Pool
	dbMutex     sync.RWMutex
	mockDB      = make(map[string]interface{})
	metricsLock sync.Mutex
	metrics     = struct {
		TotalRequests      int64
		SuccessfulRequests int64
		FailedRequests     int64
		AvgResponseTime    float64
		RequestTimes       []time.Duration
	}{
		RequestTimes: make([]time.Duration, 0, 1000),
	}
)

func init() {
	// 初始化协程池
	options := ants.Options{
		ExpiryDuration:   time.Minute * 10,
		PreAlloc:         true,
		MaxBlockingTasks: 1000,
		Nonblocking:      false,
	}

	p, err := ants.NewPool(100000, ants.WithOptions(options))
	if err != nil {
		log.Fatal(err)
	}
	pool = p

	// 初始化随机种子
	rand.Seed(time.Now().UnixNano())

	// 初始化一些模拟数据
	mockDB["users"] = []map[string]interface{}{
		{"id": 1, "name": "User1", "email": "user1@example.com"},
		{"id": 2, "name": "User2", "email": "user2@example.com"},
	}
}

// 模拟CPU密集型操作 - 增加复杂度
func cpuIntensiveTask() float64 {
	result := 0.0
	for i := 0; i < 1000000; i++ {
		result += math.Sqrt(float64(i))
		if i%10000 == 0 {
			result = result * math.Sin(result) / 10
		}
	}
	return result
}

// 模拟IO密集型操作 - 增加随机性
func ioIntensiveTask() {
	// 随机等待50-150ms
	waitTime := time.Duration(50+rand.Intn(100)) * time.Millisecond
	time.Sleep(waitTime)
}

// 模拟数据库读操作
func dbReadOperation(key string) (interface{}, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	// 模拟随机延迟
	time.Sleep(time.Duration(rand.Intn(30)) * time.Millisecond)

	if val, exists := mockDB[key]; exists {
		return val, nil
	}
	return nil, fmt.Errorf("key '%s' not found", key)
}

// 模拟数据库写操作
func dbWriteOperation(key string, value interface{}) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	// 模拟随机延迟
	time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)

	mockDB[key] = value
	return nil
}

// 模拟文件操作
func fileOperation(data string) error {
	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "jmeter-test-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// 写入数据
	_, err = tmpFile.WriteString(data)
	if err != nil {
		return err
	}

	// 模拟文件处理延迟
	time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)

	return nil
}

// 记录请求指标
// 请求日志中间件
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 开始时间
		start := time.Now()

		// 请求路径
		path := c.Request.URL.Path

		// 处理请求
		c.Next()

		// 结束时间
		end := time.Now()

		// 执行时间
		latency := end.Sub(start)

		// 请求方法
		method := c.Request.Method

		// 状态码
		statusCode := c.Writer.Status()

		// 客户端IP
		clientIP := c.ClientIP()

		// 打印日志
		log.Printf("%s | %3d | %13v | %15s | %s",
			method,
			statusCode,
			latency,
			clientIP,
			path,
		)
	}
}

// 记录请求指标
func recordMetrics(start time.Time, success bool) {
	metricsLock.Lock()
	defer metricsLock.Unlock()

	metrics.TotalRequests++
	if success {
		metrics.SuccessfulRequests++
	} else {
		metrics.FailedRequests++
	}

	elapsed := time.Since(start)
	metrics.RequestTimes = append(metrics.RequestTimes, elapsed)

	// 重新计算平均响应时间
	total := time.Duration(0)
	for _, t := range metrics.RequestTimes {
		total += t
	}
	metrics.AvgResponseTime = float64(total) / float64(len(metrics.RequestTimes)) / float64(time.Millisecond)

	// 限制存储的请求时间数量
	if len(metrics.RequestTimes) > 1000 {
		metrics.RequestTimes = metrics.RequestTimes[len(metrics.RequestTimes)-1000:]
	}
}

func main() {
	defer pool.Release()

	// 设置Gin为发布模式
	gin.SetMode(gin.ReleaseMode)

	// 创建带有自定义中间件的路由器
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestLogger())

	// API路由组
	api := r.Group("/api")
	{
		// 健康检查接口
		api.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok", "timestamp": time.Now().Unix()})
		})

		// CPU密集型接口
		api.GET("/cpu", func(c *gin.Context) {
			start := time.Now()
			var result float64
			var wg sync.WaitGroup
			wg.Add(1)

			err := pool.Submit(func() {
				defer wg.Done()
				result = cpuIntensiveTask()
			})

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				recordMetrics(start, false)
				return
			}

			// 等待任务完成
			wg.Wait()

			c.JSON(http.StatusOK, gin.H{
				"result":            result,
				"execution_time_ms": time.Since(start).Milliseconds(),
			})
			recordMetrics(start, true)
		})

		// IO密集型接口
		api.GET("/io", func(c *gin.Context) {
			start := time.Now()
			var wg sync.WaitGroup
			wg.Add(1)

			err := pool.Submit(func() {
				defer wg.Done()
				ioIntensiveTask()
			})

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				recordMetrics(start, false)
				return
			}

			// 等待任务完成
			wg.Wait()

			c.JSON(http.StatusOK, gin.H{
				"status":            "completed",
				"execution_time_ms": time.Since(start).Milliseconds(),
			})
			recordMetrics(start, true)
		})

		// 数据库读操作接口
		api.GET("/db/:key", func(c *gin.Context) {
			start := time.Now()
			key := c.Param("key")

			var (
				result interface{}
				err    error
			)
			var wg sync.WaitGroup
			wg.Add(1)

			err = pool.Submit(func() {
				defer wg.Done()
				result, err = dbReadOperation(key)
			})

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				recordMetrics(start, false)
				return
			}

			// 等待任务完成
			wg.Wait()

			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				recordMetrics(start, false)
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"data":              result,
				"execution_time_ms": time.Since(start).Milliseconds(),
			})
			recordMetrics(start, true)
		})

		// 数据库写操作接口
		api.POST("/db/:key", func(c *gin.Context) {
			start := time.Now()
			key := c.Param("key")

			// 读取请求体
			body, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
				recordMetrics(start, false)
				return
			}

			// 解析JSON
			var data interface{}
			if err := json.Unmarshal(body, &data); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
				recordMetrics(start, false)
				return
			}

			var wg sync.WaitGroup
			wg.Add(1)

			err = pool.Submit(func() {
				defer wg.Done()
				err = dbWriteOperation(key, data)
			})

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				recordMetrics(start, false)
				return
			}

			// 等待任务完成
			wg.Wait()

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				recordMetrics(start, false)
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status":            "success",
				"key":               key,
				"execution_time_ms": time.Since(start).Milliseconds(),
			})
			recordMetrics(start, true)
		})

		// 文件处理接口
		api.POST("/file", func(c *gin.Context) {
			start := time.Now()

			// 读取请求体
			body, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
				recordMetrics(start, false)
				return
			}

			var wg sync.WaitGroup
			wg.Add(1)

			err = pool.Submit(func() {
				defer wg.Done()
				err = fileOperation(string(body))
			})

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				recordMetrics(start, false)
				return
			}

			// 等待任务完成
			wg.Wait()

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				recordMetrics(start, false)
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status":            "success",
				"size_bytes":        len(body),
				"execution_time_ms": time.Since(start).Milliseconds(),
			})
			recordMetrics(start, true)
		})

		// 混合负载接口 - 同时执行CPU和IO操作
		api.GET("/mixed", func(c *gin.Context) {
			start := time.Now()
			var result float64

			// 使用WaitGroup等待所有任务完成
			var wg sync.WaitGroup
			wg.Add(2) // CPU和IO两个任务

			// 提交CPU密集型任务
			err1 := pool.Submit(func() {
				defer wg.Done()
				result = cpuIntensiveTask()
			})

			// 提交IO密集型任务
			err2 := pool.Submit(func() {
				defer wg.Done()
				ioIntensiveTask()
			})

			if err1 != nil || err2 != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to submit tasks"})
				recordMetrics(start, false)
				return
			}

			// 等待所有任务完成
			wg.Wait()

			c.JSON(http.StatusOK, gin.H{
				"status":            "completed",
				"cpu_result":        result,
				"execution_time_ms": time.Since(start).Milliseconds(),
			})
			recordMetrics(start, true)
		})

		// 指标统计接口
		api.GET("/metrics", func(c *gin.Context) {
			metricsLock.Lock()
			defer metricsLock.Unlock()

			c.JSON(http.StatusOK, gin.H{
				"total_requests":       metrics.TotalRequests,
				"successful_requests":  metrics.SuccessfulRequests,
				"failed_requests":      metrics.FailedRequests,
				"avg_response_time_ms": metrics.AvgResponseTime,
				"pool_running_workers": pool.Running(),
				"pool_free_workers":    pool.Free(),
				"pool_capacity":        pool.Cap(),
			})
		})
	}

	// 向后兼容的路由 - 重定向到新API
	r.GET("/health", func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "/api/health")
	})

	r.GET("/cpu", func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "/api/cpu")
	})

	r.GET("/io", func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "/api/io")
	})

	// 启动服务器
	addr := ":8080"
	fmt.Printf("Server is running on http://localhost%s\n", addr)
	fmt.Printf("API endpoints available at http://localhost%s/api/\n", addr)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
