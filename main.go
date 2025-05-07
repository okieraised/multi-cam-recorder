package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/urfave/cli/v3"
	"image"
	"image/color"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"gocv.io/x/gocv"
)

type Config struct {
	MaxCam        int
	OutputDir     string
	Width         float64
	Height        float64
	FPS           float64
	EnableOverlay bool
}

var (
	logger *slog.Logger
	config *Config
)

func init() {
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{AddSource: true}))
	slog.SetDefault(logger)

	config = &Config{
		MaxCam:        10,
		OutputDir:     "./output",
		Width:         640.0,
		Height:        480.0,
		FPS:           30,
		EnableOverlay: true,
	}
}

func parseConfig(cmd *cli.Command) {

	if cmd.IsSet("max-cam") {
		config.MaxCam = cmd.IntArg("max-cam")
	}

	if cmd.IsSet("output-dir") {
		config.OutputDir = cmd.StringArg("output-dir")
	}

	if cmd.IsSet("width") {
		config.Width = cmd.Float64Arg("width")
	}

	if cmd.IsSet("height") {
		config.Height = cmd.Float64Arg("height")
	}

	if cmd.IsSet("fps") {
		config.FPS = cmd.Float64Arg("fps")
	}
	if cmd.IsSet("enable-overlay") {
		config.EnableOverlay = cmd.Bool("enable-overlay")
	}
}

type Camera struct {
	ID       int
	Capture  *gocv.VideoCapture
	Writer   *gocv.VideoWriter
	Frame    gocv.Mat
	FPS      float64
	Filename string
	Rotation int
	Mirror   bool
}

func main() {
	cmd := &cli.Command{
		Name:      "mCamRecorder",
		Usage:     "A CLI for multi camera recordings",
		Version:   "v0.1.0",
		Copyright: "(c) 2025 Thomas Pham",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "max-cam", Usage: "Maximum number of cameras to scan", Aliases: []string{"n"}, Validator: func(i int) error {
				if i <= 0 {
					return errors.New("number of camera must be greater than zero")
				}
				return nil
			}},
			&cli.StringFlag{Name: "output-dir", Usage: "Directory to save output", Aliases: []string{"o"}},
			&cli.Float64Flag{Name: "width", Usage: "Video capture width", Aliases: []string{"w"}, Validator: func(f float64) error {
				if f <= 0 {
					return errors.New("width must be greater than zero")
				}
				return nil
			}},
			&cli.Float64Flag{Name: "height", Usage: "Video capture height", Aliases: []string{"h"}, Validator: func(f float64) error {
				if f <= 0 {
					return errors.New("height must be greater than zero")
				}
				return nil
			}},
			&cli.Float64Flag{Name: "fps", Usage: "Frames per second", Validator: func(f float64) error {
				if f <= 0 {
					return errors.New("fps must be greater than zero")
				}
				return nil
			}},
			&cli.BoolFlag{Name: "enable-overlay", Usage: "Enable overlay text", Aliases: []string{"ovl"}},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cli.DefaultAppComplete(ctx, cmd)
			err := cli.ShowAppHelp(cmd)
			if err != nil {
				return err
			}
			cli.ShowVersion(cmd)
			parseConfig(cmd)

			startCapture()

			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		logger.Error(err.Error())
	}

}

func openCamera(id int, width, height, fps float64) (*Camera, error) {
	capture, err := gocv.OpenVideoCapture(id)
	if err != nil || !capture.IsOpened() {
		return nil, fmt.Errorf("could not open camera %d", id)
	}
	capture.Set(gocv.VideoCaptureFrameWidth, width)
	capture.Set(gocv.VideoCaptureFrameHeight, height)

	mat := gocv.NewMat()

	outDir := config.OutputDir
	_ = os.MkdirAll(outDir, os.ModePerm)
	filename := filepath.Join(outDir, fmt.Sprintf("camera_%d_%d.mp4", id, time.Now().Unix()))
	writer, err := gocv.VideoWriterFile(filename, "mp4v", fps, int(width), int(height), true)
	if err != nil {
		_ = capture.Close()
		return nil, err
	}

	return &Camera{
		ID:       id,
		Capture:  capture,
		Writer:   writer,
		Frame:    mat,
		FPS:      fps,
		Filename: filename,
	}, nil
}

func detectVideoDevices(max int) []int {
	var devices []int
	for i := 0; i < max; i++ {
		capture, err := gocv.OpenVideoCapture(i)
		if err != nil {

			continue
		}

		if capture.IsOpened() {
			devices = append(devices, i)
			cErr := capture.Close()
			if cErr != nil {
				continue
			}
		}
	}
	return devices
}

func addOverlay(mat *gocv.Mat, camID int, fps float64) {
	text := fmt.Sprintf("Cam %d | %s | %.2f FPS", camID, time.Now().Format("2006-01-02 15:04:05.000"), fps)
	err := gocv.PutText(mat, text, image.Pt(10, 20), gocv.FontHersheyPlain, 1.1, color.RGBA{R: 255}, 2)
	if err != nil {
		logger.Error(fmt.Sprintf("Error adding overlay: %v.", err))
	}
}

func tileGrid(mats []gocv.Mat, width, height int) gocv.Mat {
	n := len(mats)
	if n == 0 {
		return gocv.NewMatWithSize(height, width, gocv.MatTypeCV8UC3)
	}
	cols := (n + 1) / 2
	rows := 2

	grid := make([][]gocv.Mat, rows)
	for r := 0; r < rows; r++ {
		grid[r] = make([]gocv.Mat, cols)
		for c := 0; c < cols; c++ {
			idx := r*cols + c
			if idx < len(mats) {
				grid[r][c] = mats[idx]
			} else {
				grid[r][c] = gocv.NewMatWithSize(height, width, gocv.MatTypeCV8UC3)
			}
		}
	}

	rowsMat := make([]gocv.Mat, 0, rows)
	for _, row := range grid {
		rowMat := row[0].Clone()
		for i := 1; i < len(row); i++ {
			result := gocv.NewMat()
			err := gocv.Hconcat(rowMat, row[i], &result)
			if err != nil {
				logger.Error(fmt.Sprintf("Error adding horizontal tile: %v.", err))
			}
			_ = rowMat.Close()
			rowMat = result
		}
		rowsMat = append(rowsMat, rowMat)
	}

	final := rowsMat[0].Clone()
	for i := 1; i < len(rowsMat); i++ {
		result := gocv.NewMat()
		err := gocv.Vconcat(final, rowsMat[i], &result)
		if err != nil {
			logger.Error(fmt.Sprintf("Error adding horizontal tile: %v.", err))
		}
		_ = final.Close()
		final = result
	}
	return final
}

func (c *Camera) transformFrame(mat *gocv.Mat, angle int, mirror bool) gocv.Mat {
	processed := mat.Clone()

	switch angle {
	case 180:
		tmp := gocv.NewMat()
		err := gocv.Flip(processed, &tmp, -1)
		if err != nil {
			logger.Error(err.Error())
		}
		err = processed.Close()
		if err != nil {
			logger.Error(err.Error())
		}
		processed = tmp
	}

	if mirror {
		tmp := gocv.NewMat()
		err := gocv.Flip(processed, &tmp, 1)
		if err != nil {
			logger.Error(err.Error())
		}
		err = processed.Close()
		if err != nil {
			logger.Error(err.Error())
		}
		processed = tmp
	}

	return processed
}

func saveSnapshot(mat gocv.Mat, camID int) {
	snapDir := "snapshots"
	_ = os.MkdirAll(snapDir, os.ModePerm)
	filename := filepath.Join(snapDir, fmt.Sprintf("snapshot_cam%d_%d.jpg", camID, time.Now().Unix()))
	if ok := gocv.IMWrite(filename, mat); ok {
		logger.Info(fmt.Sprintf("Saved snapshot: %s", filename))
	} else {
		logger.Info("Failed to save snapshot.")
	}
}

func startCapture() {
	logger.Info("Started detecting available cameras.")
	deviceIDs := detectVideoDevices(config.MaxCam)
	if len(deviceIDs) == 0 {
		logger.Info("No video devices found.")
		return
	}
	logger.Info(fmt.Sprintf("Found %d camera(s): %v.", len(deviceIDs), deviceIDs))

	var cameras []*Camera
	for _, id := range deviceIDs {
		cam, err := openCamera(id, config.Width, config.Height, config.FPS)
		if err != nil {
			logger.Error(err.Error())
			continue
		}
		logger.Info(fmt.Sprintf("Opened cam %d will write to %s.", id, cam.Filename))
		cameras = append(cameras, cam)
	}
	if len(cameras) == 0 {
		logger.Info("No cameras opened.")
		return
	}

	defer func() {
		for _, cam := range cameras {
			_ = cam.Capture.Close()
			_ = cam.Writer.Close()
			_ = cam.Frame.Close()
		}
	}()

	window := gocv.NewWindow("Multi-Camera Viewer")
	defer func(window *gocv.Window) {
		cErr := window.Close()
		if cErr != nil {
			logger.Error(fmt.Sprintf("Failed to close window: %v.", cErr))
		}
	}(window)

	activeCam := -1
	logger.Info("Recording. Press ESC to stop. Press 1–9 to switch, 0 for grid, s to snapshot, r/R to rotate, m/M to mirror.")

	for {
		var tiles []gocv.Mat
		for _, cam := range cameras {
			if ok := cam.Capture.Read(&cam.Frame); !ok || cam.Frame.Empty() {
				tile := gocv.NewMatWithSize(int(config.Height), int(config.Width), gocv.MatTypeCV8UC3)
				tiles = append(tiles, tile)
				continue
			}
			transformed := cam.transformFrame(&cam.Frame, cam.Rotation, cam.Mirror)
			if config.EnableOverlay {
				addOverlay(&transformed, cam.ID, cam.FPS)
			}

			err := cam.Writer.Write(transformed)
			if err != nil {
				logger.Error(fmt.Sprintf("Failed to write camera %d: %v.", cam.ID, err))
			}
			tiles = append(tiles, transformed)
		}

		var output gocv.Mat
		if activeCam >= 0 && activeCam < len(cameras) {
			output = tiles[activeCam].Clone()
		} else {
			output = tileGrid(tiles, int(config.Width), int(config.Height))
		}

		err := window.IMShow(output)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to display window: %v.", err))
		}
		key := window.WaitKey(1)
		if key == 27 {
			err := output.Close()
			if err != nil {
				logger.Error(fmt.Sprintf("Failed to close output: %v.", err))
			}
			break
		}
		if key >= '0' && key <= '9' {
			activeCam = key - '0'
			if activeCam >= len(cameras) {
				activeCam = -1
			}
		}

		if key == 's' || key == 'S' {
			if activeCam >= 0 && activeCam < len(cameras) {
				if config.EnableOverlay {
					addOverlay(&cameras[activeCam].Frame, cameras[activeCam].ID, cameras[activeCam].FPS)
				}
				saveSnapshot(cameras[activeCam].Frame, cameras[activeCam].ID)
			} else {
				for _, cam := range cameras {
					if config.EnableOverlay {
						addOverlay(&cam.Frame, cam.ID, cam.FPS)
					}
					saveSnapshot(cam.Frame, cam.ID)
				}
			}
		}
		if key == 'r' || key == 'R' {
			for _, cam := range cameras {
				cam.Rotation = (cam.Rotation + 180) % 360
				logger.Info(fmt.Sprintf("Cam %d rotation: %d°.", cam.ID, cam.Rotation))
			}
		}

		if key == 'm' || key == 'M' {
			for _, cam := range cameras {
				cam.Mirror = !cam.Mirror
				state := "OFF"
				if cam.Mirror {
					state = "ON"
				}
				logger.Info(fmt.Sprintf("Cam %d mirror: %s.", cam.ID, state))
			}
		}

		err = output.Close()
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to close output: %v.", err))
		}
		for _, t := range tiles {
			err = t.Close()
			if err != nil {
				logger.Error(fmt.Sprintf("Failed to close tile: %v.", err))
			}
		}
	}
}
