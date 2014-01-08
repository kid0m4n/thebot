package main

import (
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/kid0m4n/go-rpi/controller/pca9685"
	"github.com/kid0m4n/go-rpi/i2c"
	"github.com/kid0m4n/go-rpi/motion/servo"
	"github.com/kid0m4n/go-rpi/sensor/l3gd20"
)

var (
	threshold        = flag.Int("threshold", 30, "safe distance to stop the car")
	camWidth         = flag.Int("camw", 640, "width of the captured camera image")
	camHeight        = flag.Int("camh", 480, "height of the captured camera image")
	camFps           = flag.Int("fps", 4, "fps for camera")
	fakeCar          = flag.Bool("fcr", false, "fake the car")
	fakeCam          = flag.Bool("fcm", false, "fake the camera")
	echoPinNumber    = flag.Int("epn", 10, "GPIO pin connected to the echo pad")
	triggerPinNumber = flag.Int("tpn", 9, "GPIO pin connected to the trigger pad")
)

func main() {
	log.Print("main: starting up")

	flag.Parse()

	var cam Camera = NullCamera
	if !*fakeCam {
		cam = NewCamera(*camWidth, *camHeight, *camFps)
	}
	defer cam.Close()
	cam.Run()

	comp := NewCompass(i2c.Default)
	defer comp.Close()
	comp.Run()

	rf := NewRangeFinder(*echoPinNumber, *triggerPinNumber)

	pwmServo := pca9685.New(i2c.Default, 0x42, 50)
	defer pwmServo.Close()
	pwmMotor := pca9685.New(i2c.Default, 0x41, 1000)
	defer pwmMotor.Close()

	servo := servo.New(pwmServo, 50, 0, 1, 2.5)

	frontWheel := &frontWheel{servo}
	defer frontWheel.Turn(0)

	engine := NewEngine(15, pwmMotor)
	defer engine.Stop()

	gyro := NewGyroscope(i2c.Default, l3gd20.R250DPS)

	var car Car = NullCar
	if !*fakeCar {
		car = NewCar(i2c.Default, cam, comp, rf, gyro, frontWheel, engine)
	}

	ws := NewWebServer(car)
	ws.Run()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, os.Kill)
	<-quit

	log.Print("main: all done")
}