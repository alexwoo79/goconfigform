package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Duration 表示时间段
type Duration struct {
	hours   int
	minutes int
	seconds int
}

// TotalMinutes 计算总分钟数
func (d *Duration) TotalMinutes() float64 {
	return float64(d.hours*60+d.minutes) + float64(d.seconds)/60.0
}

// TotalSeconds 计算总秒数
func (d *Duration) TotalSeconds() int {
	return d.hours*3600 + d.minutes*60 + d.seconds
}

// Pace 计算配速（每公里所需的分钟数），传入距离（公里）
func (d *Duration) Pace(distanceKm float64) float64 {
	totalMinutes := d.TotalMinutes()
	return totalMinutes / distanceKm
}

// String 实现Stringer接口，返回格式化的持续时间字符串
func (d *Duration) String() string {
	return fmt.Sprintf("%dh %dm %ds", d.hours, d.minutes, d.seconds)
}

// ParseDuration 解析 HH:MM:SS 或 MM:SS 或 SS 格式的时间字符串
func ParseDuration(s string) *Duration {
	parts := strings.Split(s, ":")

	switch len(parts) {
	case 1: // 只有秒
		seconds, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil
		}
		return &Duration{0, 0, seconds}
	case 2: // MM:SS
		minutes, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil
		}
		seconds, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil
		}
		return &Duration{0, minutes, seconds}
	case 3: // HH:MM:SS
		hours, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil
		}
		minutes, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil
		}
		seconds, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil
		}
		return &Duration{hours, minutes, seconds}
	default:
		return nil
	}
}

// ParsePace 将时长字符串和距离计算出配速，返回格式为"xx分xx秒"
func ParsePace(durationStr string, distanceKm float64) string {
	d := ParseDuration(durationStr)
	if d == nil {
		return "解析时间失败"
	}

	totalSeconds := d.TotalSeconds()
	secondsPerKm := float64(totalSeconds) / distanceKm

	// 计算每公里的分钟数和秒数
	minutesPerKm := int(secondsPerKm) / 60
	remainingSecondsPerKm := int(secondsPerKm) % 60

	return fmt.Sprintf("%d分%02d秒", minutesPerKm, remainingSecondsPerKm)
}

// 添加TrainingRecord结构体来存储完整的训练记录
type TrainingRecord struct {
	Date       string
	Duration   string
	DistanceKm float64
	Pace       string
	Notes      string
}

// 修改HTTP处理函数，添加训练记录功能
func runningPaceHandler(c *gin.Context) {
	var resultHTML string
	var records []TrainingRecord

	if c.Request.Method == http.MethodPost {
		durationStr := c.Request.FormValue("duration")
		distanceStr := c.Request.FormValue("distance")
		dateStr := c.Request.FormValue("date")
		notes := c.Request.FormValue("notes")

		distanceKm, err := strconv.ParseFloat(distanceStr, 64)
		if err != nil || distanceKm <= 0 {
			resultHTML = `<div class="error">无效的距离值</div>`
		} else {
			paceStr := ParsePace(durationStr, distanceKm)

			record := TrainingRecord{
				Date:       dateStr,
				Duration:   durationStr,
				DistanceKm: distanceKm,
				Pace:       paceStr,
				Notes:      notes,
			}

			records = append(records, record)

			resultHTML = fmt.Sprintf(`
<div class="result">
    <h3>配速计算结果</h3>
    <p><strong>日期:</strong> %s</p>
    <p><strong>时间:</strong> %s</p>
    <p><strong>距离:</strong> %.3f 公里</p>
    <p><strong>配速:</strong> %s/公里</p>
    <p><strong>备注:</strong> %s</p>
</div>`, dateStr, durationStr, distanceKm, paceStr, notes)
		}
	}

	formHTML := `
<!DOCTYPE html>
<html>
<head>
    <title>长跑运动员数据记录器</title>
    <style>
        body { font-family: Arial, sans-serif; max-width: 600px; margin: 50px auto; padding: 20px; }
        .form-group { margin-bottom: 15px; }
        label { display: block; margin-bottom: 5px; font-weight: bold; }
        input[type="text"], input[type="number"], input[type="date"] { width: 80%; padding: 8px; font-size: 16px; }
        button { background-color: #4CAF50; color: white; padding: 10px 20px; border: none; cursor: pointer; font-size: 16px; }
        .result { margin-top: 20px; padding: 15px; background-color: #e8f5e8; border-radius: 5px; border-left: 5px solid #4CAF50; }
        .error { margin-top: 20px; padding: 15px; background-color: #ffe8e8; border-radius: 5px; border-left: 5px solid #f44336; }
        .record-history { margin-top: 30px; }
        .record-item { padding: 10px; margin: 10px 0; background-color: #f5f5f5; border-radius: 3px; }
    </style>
</head>
<body>
    <h1>长跑运动员数据记录器</h1>
    <form method="POST" action="/pace">
        <div class="form-group">
            <label for="date">训练日期:</label>
            <input type="date" id="date" name="date" value="` + getCurrentDate() + `" required>
        </div>
        <div class="form-group">
            <label for="duration">完成时间 (格式: HH:MM:SS, MM:SS 或 S):</label>
            <input type="text" id="duration" name="duration" placeholder="例如: 1:23:30 或 45:20 或 3600" required>
        </div>
        <div class="form-group">
            <label for="distance">跑步距离 (公里):</label>
            <input type="number" id="distance" name="distance" step="0.001" min="0.001" placeholder="例如: 10.5" required>
        </div>
        <div class="form-group">
            <label for="notes">训练备注:</label>
            <input type="text" id="notes" name="notes" placeholder="例如: 晨跑，感觉良好">
        </div>
        <button type="submit">记录训练数据</button>
    </form>`

	if resultHTML != "" {
		formHTML += resultHTML
	}

	if len(records) > 0 {
		formHTML += `<div class="record-history"><h3>近期训练记录</h3>`
		for _, record := range records {
			formHTML += fmt.Sprintf(`
<div class="record-item">
    <p><strong>%s</strong> - 距离: %.3fkm | 时间: %s | 配速: %s/km | 备注: %s</p>
</div>`, record.Date, record.DistanceKm, record.Duration, record.Pace, record.Notes)
		}
		formHTML += `</div>`
	}

	formHTML += `
</body>
</html>`

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(200, formHTML)
}

// 获取当前日期，格式为YYYY-MM-DD
func getCurrentDate() string {
	// 这里简单返回当前日期，实际应该使用time.Now().Format("2006-01-02")
	// 为了简化实现，这里直接返回当前日期字符串
	return time.Now().Format("2006-01-02")
}
