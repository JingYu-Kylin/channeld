package channeld

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"

	"channeld.clewcat.com/channeld/proto"
	"github.com/pkg/profile"
)

type GlobalSettingsType struct {
	Development   bool
	LogLevel      *NullableInt // zapcore.Level
	LogFile       *NullableString
	ProfileOption func(*profile.Profile)
	ProfilePath   string

	ServerNetwork string
	ServerAddress string
	ServerFSM     string

	ClientNetwork string
	ClientAddress string
	ClientFSM     string

	CompressionType proto.CompressionType

	ChannelSettings map[proto.ChannelType]ChannelSettingsType
}

type ChannelSettingsType struct {
	TickIntervalMs          uint
	DefaultFanOutIntervalMs uint32
}

var GlobalSettings = GlobalSettingsType{
	LogLevel:        &NullableInt{},
	LogFile:         &NullableString{},
	CompressionType: proto.CompressionType_NO_COMPRESSION,
	ChannelSettings: map[proto.ChannelType]ChannelSettingsType{
		proto.ChannelType_GLOBAL: {
			TickIntervalMs:          10,
			DefaultFanOutIntervalMs: 20,
		},
	},
}

type NullableInt struct {
	Value    int
	HasValue bool
}

func (i NullableInt) String() string {
	if i.HasValue {
		return strconv.Itoa(i.Value)
	} else {
		return ""
	}
}

func (i *NullableInt) Set(s string) error {
	val, err := strconv.Atoi(s)
	if err == nil {
		i.Value = val
		i.HasValue = true
	}
	return err
}

type NullableString struct {
	Value    string
	HasValue bool
}

func (ns NullableString) String() string {
	return ns.Value
}

func (ns *NullableString) Set(s string) error {
	ns.Value = s
	ns.HasValue = true
	return nil
}

func (s *GlobalSettingsType) ParseFlag() error {
	flag.BoolVar(&s.Development, "dev", false, "run in development mode?")
	flag.Var(s.LogLevel, "loglevel", "the log level, -1 = Debug, 0 = Info, 1= Warn, 2 = Error, 3 = Panic")
	//flag.Var(stringPtrFlag{s.LogFile, fmt.Sprintf("logs/%s.log", time.Now().Format("20060102150405"))}, "logfile", "file path to store the log")
	flag.Var(s.LogFile, "logfile", "file path to store the log")
	flag.Func("profile", "available options: cpu, mem, goroutine", func(str string) error {
		switch strings.ToLower(str) {
		case "cpu":
			s.ProfileOption = profile.CPUProfile
		case "mem":
			s.ProfileOption = profile.MemProfile
		case "goroutine":
			s.ProfileOption = profile.GoroutineProfile
		default:
			return fmt.Errorf("invalid profile type: %s", str)
		}
		return nil
	})
	flag.StringVar(&s.ProfilePath, "profilepath", "profiles", "the path to store the profile output files")

	flag.StringVar(&s.ServerNetwork, "sn", "tcp", "the network type for the server connections")
	flag.StringVar(&s.ServerAddress, "sa", ":11288", "the network address for the server connections")
	flag.StringVar(&s.ServerFSM, "sfsm", "config/server_authoratative_fsm.json", "the path to the server FSM config")

	flag.StringVar(&s.ClientNetwork, "cn", "tcp", "the network type for the client connections")
	flag.StringVar(&s.ClientAddress, "ca", ":12108", "the network address for the client connections")
	flag.StringVar(&s.ClientFSM, "cfsm", "config/client_non_authoratative_fsm.json", "the path to the client FSM config")

	ct := flag.Uint("ct", 0, "the compression type, 0 = No, 1 = Snappy")

	chs := flag.String("chs", "config/channel_settings_hifi.json", "the path to the channel settings file")

	flag.Parse()

	if ct != nil {
		s.CompressionType = proto.CompressionType(*ct)
	}

	chsData, err := ioutil.ReadFile(*chs)
	if err == nil {
		if err := json.Unmarshal(chsData, &GlobalSettings.ChannelSettings); err != nil {
			return fmt.Errorf("failed to unmarshall channel settings: %v", err)
		}
	} else {
		return fmt.Errorf("failed to read channel settings: %v", err)
	}

	return nil
}

func (s GlobalSettingsType) GetChannelSettings(t proto.ChannelType) ChannelSettingsType {
	settings, exists := s.ChannelSettings[t]
	if !exists {
		settings = s.ChannelSettings[proto.ChannelType_GLOBAL]
	}
	return settings
}
