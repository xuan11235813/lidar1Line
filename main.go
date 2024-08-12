package main

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args
	if len(args) == 1 {
		startNetLidar()
	} else {
		fmt.Println(args[1])
		startFileSimulation(args[1])
	}
}
func startFileSimulation(fileName string) {
	file, err := os.Open(fileName)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer file.Close()
	byteWorker := StartByteWorker()
	StarEstimateWorker(byteWorker.chDataFrame, 720)
	for {
		tmp := make([]byte, 256)
		n, err := file.Read(tmp)
		if err != nil {
			if err != io.EOF {
				fmt.Println("read error:", err)
			}
			break
		}

		byteWorker.chBufferByte <- tmp[:n]
		time.Sleep(1 * time.Millisecond)
	}
}

func startNetLidar() {
	conn, err := net.Dial("tcp", "192.168.80.6:6008")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer conn.Close()
	byteWorker := StartByteWorker()
	StarEstimateWorker(byteWorker.chDataFrame, 361)

	for {
		tmp := make([]byte, 256)
		n, err := conn.Read(tmp)
		if err != nil {
			if err != io.EOF {
				fmt.Println("read error:", err)
			}
			break
		}

		byteWorker.chBufferByte <- tmp[:n]
		//buf = append(buf, tmp[:n]...)
	}
}

// CSVFileToMap  reads csv file into slice of map
// slice is the line number
// map[string]string where key is column name
func CSVFileToMap(filePath string) (returnMap []map[string]string, err error) {

	// read csv file
	csvfile, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf(err.Error())
	}

	defer csvfile.Close()

	reader := csv.NewReader(csvfile)

	rawCSVdata, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf(err.Error())
	}

	header := []string{} // holds first row (header)
	for lineNum, record := range rawCSVdata {

		// for first row, build the header slice
		if lineNum == 0 {
			for i := 0; i < len(record); i++ {
				header = append(header, strings.TrimSpace(record[i]))
			}

		} else {
			// for each cell, map[string]string k=header v=value
			line := map[string]string{}
			for i := 0; i < len(record); i++ {
				line[header[i]] = record[i]
			}
			returnMap = append(returnMap, line)
		}
	}

	return
}

// MapToCSVFile  writes slice of map into csv file
// filterFields filters to only the fields in the slice, and maintains order when writing to file
func MapToCSVFile(inputSliceMap []map[string]string, filePath string, filterFields []string) (err error) {

	var headers []string  // slice of each header field
	var line []string     // slice of each line field
	var csvLine string    // string of line converted to csv
	var CSVContent string // final output of csv containing header and lines

	// iter over slice to get all possible keys (csv header) in the maps
	// using empty Map[string]struct{} to get UNIQUE Keys; no value needed
	var headerMap = make(map[string]struct{})
	for _, record := range inputSliceMap {
		for k := range record {
			headerMap[k] = struct{}{}
		}
	}

	// convert unique headersMap to slice
	for headerValue := range headerMap {
		headers = append(headers, headerValue)
	}

	// filter to filteredFields and maintain order
	var filteredHeaders []string
	if len(filterFields) > 0 {
		for _, filterField := range filterFields {
			for _, headerValue := range headers {
				if filterField == headerValue {
					filteredHeaders = append(filteredHeaders, headerValue)
				}
			}
		}
	} else {
		filteredHeaders = append(filteredHeaders, headers...)
		sort.Strings(filteredHeaders) // alpha sort headers
	}

	// write headers as the first line
	csvLine, _ = WriteAsCSV(filteredHeaders)
	CSVContent += csvLine + "\n"

	// iter over inputSliceMap to get values for each map
	// maintain order provided in header slice
	// write to csv
	for _, record := range inputSliceMap {
		line = []string{}

		// lines
		for k := range filteredHeaders {
			line = append(line, record[filteredHeaders[k]])
		}
		csvLine, _ = WriteAsCSV(line)
		CSVContent += csvLine + "\n"
	}

	// make the dir incase it's not there
	err = os.MkdirAll(filepath.Dir(filePath), os.ModePerm)
	if err != nil {
		return err
	}

	// write out the csv contents to file
	ioutil.WriteFile(filePath, []byte(CSVContent), os.FileMode(0644))
	if err != nil {
		return err
	}

	return
}

func WriteAsCSV(vals []string) (string, error) {
	b := &bytes.Buffer{}
	w := csv.NewWriter(b)
	err := w.Write(vals)
	if err != nil {
		return "", err
	}
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n"), nil
}

type ByteWorker struct {
	chBufferByte chan []byte
	chFrame      chan []byte
	chDataFrame  chan []float64
}

func StartByteWorker() *ByteWorker {
	w := &ByteWorker{}
	w.chBufferByte = make(chan []byte, 6)
	w.chFrame = make(chan []byte, 6)
	w.chDataFrame = make(chan []float64, 6)
	go w.Implement()
	go w.ToNum()
	return w
}

func (w *ByteWorker) Implement() {
	var recBytes []byte
	for {
		inputBytes := <-w.chBufferByte

		for _, byteItem := range inputBytes {
			if len(recBytes) > 10 {
				recBytes = append(recBytes, byteItem)
				N := len(recBytes)
				if recBytes[N-10] == 0x02 && recBytes[N-9] == 0x05 && recBytes[N-7] == 0xfe && recBytes[N-5] == 0xfe && recBytes[N-3] == 0xfe && recBytes[N-1] == 0xfe {
					recHead := recBytes[N-10 : N]
					w.chFrame <- recBytes[:len(recBytes)-10]
					recBytes = recHead
				}
			} else {
				recBytes = append(recBytes, byteItem)
			}
		}
	}
}

func (w *ByteWorker) ToNum() {
	for testBytes := range w.chFrame {
		if len(testBytes) == 741 {
			N := len(testBytes)
			if testBytes[N-1] == 0x0a && testBytes[N-2] == 0x0d {
				var dataFrame []float64
				for i := 11; i < 732; i = i + 2 {
					dataFrame = append(dataFrame, float64(binary.BigEndian.Uint16(testBytes[i:i+2]))/1000.0)
				}
				w.chDataFrame <- dataFrame
			}
		}
		if len(testBytes) == 1460 {

			N := len(testBytes)
			if testBytes[N-1] == 0x0a && testBytes[N-2] == 0x0d {
				var dataFrame []float64
				for i := 11; i < 1450; i = i + 2 {
					dataFrame = append(dataFrame, float64(binary.BigEndian.Uint16(testBytes[i:i+2]))/1000.0)
				}
				w.chDataFrame <- dataFrame
			}
		}
	}
}

type Point2D struct {
	X          float64
	Y          float64
	R          float64
	index      int
	distToLine float64
}

type BackLine struct {
	//y = kx +b
	K float64
	b float64
}

type FrameCapture struct {
	Capture       []Point2D
	LeftPoint     Point2D
	RightPoint    Point2D
	Width         float64
	Height        float64
	Length        int32
	bestFitIndex  int
	bestFitLength int
}

type VehicleCapture struct {
	Captures        []FrameCapture
	EstimatedHeight float64
	EstimatedWidth  float64
	isUpdated       bool
	ObjectNum       int
}

type EstimateWorker struct {
	InputChannel    chan []float64
	ChBackground    chan []float64
	ChSignalBack    chan BackLine
	ChVehicles      chan VehicleCapture
	BackgroundAngle []float64
	BackLength      int

	BackLineIsReady   bool
	BackgroundIsReady bool
}

func StarEstimateWorker(input chan []float64, backLength int) *EstimateWorker {
	w := &EstimateWorker{}
	w.InputChannel = input
	w.BackLength = backLength
	w.ChSignalBack = make(chan BackLine, 1)
	w.ChBackground = make(chan []float64, 3)
	w.ChVehicles = make(chan VehicleCapture, 3)
	w.BackLineIsReady = false
	w.BackgroundIsReady = false

	for i := 0; i < w.BackLength; i++ {
		w.BackgroundAngle = append(w.BackgroundAngle, 0.0)
	}
	go w.calculateTheBackground()
	go w.VehicleCuts()
	go w.VehicleProcess()
	return w
}

/***
 *						   ^	 Y axis
 *-------------------------|--------------------Lane
 *     |		   |       |
 *     |  vehicle  |       |
 *     |-----------|       |
 *  					   |
 *	grad -90(0)			   |						grad 90(180)
 *_________________________|______________________________> X axis
 */

func (w *EstimateWorker) calculateTheBackground() {
	fmt.Println("waiting")
	var maximalLidarDistance float64 = 15.0
	var flag int32 = 0
	var nullChangeTimes int32 = 0
	var waitingFrameNum = 0
	for dataFrame := range w.InputChannel {
		switch flag {
		case 0:
			{
				var changedNum int32 = 0
				for i := 0; i < len(w.BackgroundAngle); i++ {
					if dataFrame[i] >= 0 && dataFrame[i] <= maximalLidarDistance {
						if math.Abs(float64(dataFrame[i]-w.BackgroundAngle[i])) >= 0.05 {
							if dataFrame[i] > w.BackgroundAngle[i] {
								w.BackgroundAngle[i] = dataFrame[i]
								changedNum += 1
							}
						} else {
							if dataFrame[i] > w.BackgroundAngle[i] {
								var rate float64 = 0.5
								w.BackgroundAngle[i] = dataFrame[i]*rate + (1-rate)*w.BackgroundAngle[i]
							} else {
								var rate float64 = 0.2
								w.BackgroundAngle[i] = dataFrame[i]*rate + (1-rate)*w.BackgroundAngle[i]
							}
						}
					}
				}
				if changedNum == 0 {
					nullChangeTimes += 1
				}
				if nullChangeTimes >= 30 {
					/* BE CAUTIOUS */
					nullChangeTimes = 0
					flag = 1
					fmt.Println("finish back point generation")
				}
			}
		case 1:
			{

				/* calculate the back line */
				var angleInterval float64 = 0.5
				if w.BackLength == 720 {
					angleInterval = 0.25
				}
				var sum_x, sum_y, sum_xx, sum_xy, N float64 = 0.0, 0.0, 0.0, 0.0, 0.0

				for i := 0; i < len(w.BackgroundAngle); i++ {
					var p Point2D
					rad := (angleInterval*float64(i) - 90.0) / 180.0 * math.Pi
					p.Y = math.Cos(rad) * w.BackgroundAngle[i]
					p.X = math.Sin(rad) * w.BackgroundAngle[i]
					var centerIndex int = int(w.BackLength / 2)
					var centerLeft int = centerIndex - int(15.0/angleInterval)
					var centerRight int = centerIndex + int(15.0/angleInterval)
					if i >= centerLeft && i <= centerRight {
						sum_x += p.X
						sum_y += p.Y
						sum_xx += p.X * p.X
						sum_xy += p.X * p.Y
						N += 1.0
					}
				}

				K := (N*sum_xy - sum_x*sum_y) / (N*sum_xx - sum_x*sum_x)
				B := (sum_y / N) - (K * sum_x / N)
				fmt.Println("Y = KX+B K:=", K)
				fmt.Println("Y = KX+B B:=", B)
				var backline BackLine
				backline.b = B
				backline.K = K
				w.ChSignalBack <- backline
				back := make([]float64, len(w.BackgroundAngle))
				copy(back, w.BackgroundAngle[:])
				w.ChBackground <- back
				flag = 2
			}
		case 2:
			{
				var maximalWaitingNum = 100 * 60
				if waitingFrameNum <= maximalWaitingNum {
					waitingFrameNum++
				} else {
					waitingFrameNum = 0
					flag = 0
				}
			}
		}
	}
}

func polarToDescartes(polarData []float64, startAngle float64, intervalAngle float64) []Point2D {
	var points []Point2D
	for i := 0; i < len(polarData); i++ {
		var p Point2D
		rad := (startAngle + float64(i)*intervalAngle) / 180.0 * math.Pi
		p.Y = math.Cos(rad) * polarData[i]
		p.X = math.Sin(rad) * polarData[i]
		p.R = polarData[i]
		p.index = i
		points = append(points, p)
	}
	return points
}
func findDistance(line BackLine, point Point2D) float64 {
	ell := (line.K*point.X + line.b - point.Y) / (1 + line.K*line.K)
	return math.Abs(ell) * math.Sqrt(1+line.K*line.K)
}

func findDistance2p(p1 Point2D, p2 Point2D) float64 {
	return math.Sqrt((p1.X-p2.X)*(p1.X-p2.X) + (p1.Y-p2.Y)*(p1.Y-p2.Y))
}

func checkIfConnected(f1 FrameCapture, f2 FrameCapture) int {
	f1L := f1.LeftPoint.index
	f1R := f1.RightPoint.index
	f2L := f2.LeftPoint.index
	f2R := f2.RightPoint.index
	var fMin int = 0.0
	var fMax int = 0.0
	if f1L <= f2L {
		fMin = f1L
	} else {
		fMin = f2L
	}
	if f1R >= f2R {
		fMax = f1R
	} else {
		fMax = f2R
	}
	var sumIndex int = 0
	for i := fMin; i <= fMax; i++ {
		if (f1L <= i && i <= f1R) && (f2L <= i && i <= f2R) {
			sumIndex += 1
		}
	}
	return sumIndex
}

func fullfillTheFrameCapture(points []Point2D) FrameCapture {
	var frame FrameCapture
	frame.Capture = points
	frame.LeftPoint = points[0]
	frame.RightPoint = points[len(points)-1]
	frame.Length = int32(points[len(points)-1].index - points[0].index)
	frame.Height = 0
	frame.Width = findDistance2p(frame.LeftPoint, frame.RightPoint)
	for i := 0; i < len(frame.Capture); i++ {
		if frame.Capture[i].distToLine > frame.Height {
			frame.Height = frame.Capture[i].distToLine
		}
	}
	return frame
}
func pointsCuts(points []Point2D) []FrameCapture {
	var pointsClips []FrameCapture
	var currentClip []Point2D
	var lastIndex int32 = -1
	for i := 0; i < len(points); i++ {
		//find a start
		if lastIndex == -1 {
			currentClip = append(currentClip, points[i])
			lastIndex = int32(points[i].index)
		} else {
			if math.Abs(float64(points[i].index-int(lastIndex))) <= 2.0 {
				lastIndex = int32(points[i].index)
				currentClip = append(currentClip, points[i])
			} else {
				frame := fullfillTheFrameCapture(currentClip)
				pointsClips = append(pointsClips, frame)
				currentClip = nil
				currentClip = append(currentClip, points[i])
				lastIndex = int32(points[i].index)
			}
		}
	}
	if len(currentClip) > 1 {
		frame := fullfillTheFrameCapture(currentClip)
		pointsClips = append(pointsClips, frame)
	}
	return pointsClips
}
func (w *EstimateWorker) VehicleCuts() {
	var currentBackLine BackLine
	var lidarHeight = 0.0
	var leftXLimit = -4.0
	var rightXLimit = 4.0
	var leftAngleLimit = 40
	var rightAngleLimit = 320
	var minimalDistanceThreshold = 0.5
	var liveVehicles []VehicleCapture
	var currBackground []float64
	for i := 0; i < w.BackLength; i++ {
		currBackground = append(currBackground, 0.0)
	}
	var objectNum int = 0
	for {
		select {
		case dataFrame := <-w.InputChannel:
			{
				if w.BackLineIsReady && w.BackgroundIsReady {
					var angleInterval float64 = 0.5
					if w.BackLength == 720 {
						angleInterval = 0.25
					}

					points := polarToDescartes(dataFrame, -90.0, angleInterval)
					var pointsCleared []Point2D
					for i := 0; i < len(points); i++ {
						if points[i].X >= leftXLimit && points[i].X <= rightXLimit && i >= leftAngleLimit && i <= rightAngleLimit {
							if math.Abs(points[i].R-currBackground[i]) >= 0.1 {
								lineDistance := findDistance(currentBackLine, points[i])
								if lineDistance < lidarHeight {
									if lineDistance >= minimalDistanceThreshold {
										points[i].distToLine = lineDistance
										pointsCleared = append(pointsCleared, points[i])
									}
								}
							}
						}
					}
					pointsClips := pointsCuts(pointsCleared)
					if len(liveVehicles) == 0 {
						for _, frameCaptureItem := range pointsClips {
							var vehicleCaptureItem VehicleCapture
							vehicleCaptureItem.isUpdated = false
							vehicleCaptureItem.Captures = append(vehicleCaptureItem.Captures, frameCaptureItem)
							liveVehicles = append(liveVehicles, vehicleCaptureItem)
						}
					} else {
						for i := 0; i < len(liveVehicles); i++ {
							liveVehicles[i].isUpdated = false
						}
						/* calculate the alignment */
						for i := 0; i < len(liveVehicles); i++ {
							for j := 0; j < len(pointsClips); j++ {
								connectLength := checkIfConnected(liveVehicles[i].Captures[len(liveVehicles[i].Captures)-1], pointsClips[j])
								if connectLength > 0 {
									if connectLength > pointsClips[j].bestFitLength {
										pointsClips[j].bestFitIndex = i
										pointsClips[j].bestFitLength = connectLength
									}
								}

							}
						}
						/* add the legal points */
						for i := 0; i < len(pointsClips); i++ {
							liveVehicles[pointsClips[i].bestFitIndex].isUpdated = true
							liveVehicles[pointsClips[i].bestFitIndex].Captures = append(liveVehicles[pointsClips[i].bestFitIndex].Captures, pointsClips[i])
						}

						/* eliminate the finished vehicles */
						var vehiclesNew []VehicleCapture
						for _, item := range liveVehicles {
							if !item.isUpdated {
								item.ObjectNum = objectNum
								w.ChVehicles <- item
								objectNum += 1
							} else {
								vehiclesNew = append(vehiclesNew, item)
							}
						}
						liveVehicles = vehiclesNew
					}

				} else {
					_ = dataFrame
				}
			}
		case signal := <-w.ChSignalBack:
			{
				currentBackLine = signal
				var point Point2D
				point.X = 0
				point.Y = 0
				lidarHeight = findDistance(currentBackLine, point)
				w.BackLineIsReady = true
			}
		case back := <-w.ChBackground:
			{
				copy(currBackground, back)
				w.BackgroundIsReady = true
			}
		}
	}

}

func (w *EstimateWorker) VehicleProcess() {

	for vehicleItem := range w.ChVehicles {
		var currZ float64 = 0.0
		var stringTable []map[string]string
		for i := 0; i < len(vehicleItem.Captures); i++ {
			currZ += 0.1
			for j := 0; j < len(vehicleItem.Captures[i].Capture); j++ {
				currX := vehicleItem.Captures[i].Capture[j].X
				currY := vehicleItem.Captures[i].Capture[j].Y

				currM := map[string]string{}
				currM["x"] = strconv.FormatFloat(currX, 'f', -1, 64)
				currM["y"] = strconv.FormatFloat(currY, 'f', -1, 64)
				currM["z"] = strconv.FormatFloat(currZ, 'f', -1, 64)
				stringTable = append(stringTable, currM)
			}
		}
		var header = []string{"x", "y", "z"}
		var fileName string = "obj" + strconv.Itoa(vehicleItem.ObjectNum) + ".csv"
		MapToCSVFile(stringTable, fileName, header)
	}
}
