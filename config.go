// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers

package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/slog"
	"github.com/jessevdk/go-flags"
)

const (
	defaultConfigFilename = "gominer.conf"
	defaultLogLevel       = "info"
	defaultLogDirname     = "logs"
	defaultLogFilename    = "gominer.log"
	defaultClKernel       = "blake256.cl"
)

var (
	minerHomeDir          = dcrutil.AppDataDir("gominer", false)
	dcrdHomeDir           = dcrutil.AppDataDir("dcrd", false)
	defaultConfigFile     = filepath.Join(minerHomeDir, defaultConfigFilename)
	defaultRPCServer      = "localhost"
	defaultRPCCertFile    = filepath.Join(dcrdHomeDir, "rpc.cert")
	defaultRPCPortMainNet = "9109"
	defaultRPCPortTestNet = "19109"
	defaultRPCPortSimNet  = "19556"
	defaultAPIHost        = "localhost"
	defaultAPIPort        = "3333"
	defaultLogDir         = filepath.Join(minerHomeDir, defaultLogDirname)
	defaultAutocalibrate  = 500

	minIntensity  = 8
	maxIntensity  = 31
	minTempTarget = uint32(60)
	maxTempTarget = uint32(84)
	maxWorkSize   = uint32(0xFFFFFFFF - 255)
)

type config struct {
	ListDevices bool `short:"l" long:"listdevices" description:"List number of devices."`
	ShowVersion bool `short:"V" long:"version" description:"Display version information and exit"`

	// Config / log options
	Experimental bool   `long:"experimental" description:"enable EXPERIMENTAL features such as setting a temperature target with (-t/--temptarget) which may DAMAGE YOUR DEVICE(S)."`
	ConfigFile   string `short:"C" long:"configfile" description:"Path to configuration file"`
	LogDir       string `long:"logdir" description:"Directory to log output."`
	DebugLevel   string `short:"d" long:"debuglevel" description:"Logging level for all subsystems {trace, debug, info, warn, error, critical} -- You may also specify <subsystem>=<level>,<subsystem2>=<level>,... to set the log level for individual subsystems -- Use show to list available subsystems"`
	ClKernel     string `short:"k" long:"kernel" description:"File with cl kernel to use"`

	// Debugging options
	Profile    string `long:"profile" description:"Enable HTTP profiling on given port -- NOTE port must be between 1024 and 65536"`
	CPUProfile string `long:"cpuprofile" description:"Write CPU profile to the specified file"`
	MemProfile string `long:"memprofile" description:"Write mem profile to the specified file"`

	// Status API options
	APIListeners []string `long:"apilisten" description:"Add an interface/port to expose miner status API"`

	// RPC connection options
	RPCUser     string `short:"u" long:"rpcuser" description:"RPC username"`
	RPCPassword string `short:"P" long:"rpcpass" default-mask:"-" description:"RPC password"`
	RPCServer   string `short:"s" long:"rpcserver" description:"RPC server to connect to"`
	RPCCert     string `short:"c" long:"rpccert" description:"RPC server certificate chain for validation"`
	NoTLS       bool   `long:"notls" description:"Disable TLS"`
	Proxy       string `long:"proxy" description:"Connect via SOCKS5 proxy (eg. 127.0.0.1:9050)"`
	ProxyUser   string `long:"proxyuser" description:"Username for proxy server"`
	ProxyPass   string `long:"proxypass" default-mask:"-" description:"Password for proxy server"`

	Benchmark bool `short:"B" long:"benchmark" description:"Run in benchmark mode."`

	TestNet       bool `long:"testnet" description:"Connect to testnet"`
	SimNet        bool `long:"simnet" description:"Connect to the simulation test network"`
	TLSSkipVerify bool `long:"skipverify" description:"Do not verify tls certificates (not recommended!)"`

	Autocalibrate     string `short:"A" long:"autocalibrate" description:"Time target in milliseconds to spend executing hashes on the device during each iteration. Single global value or a comma separated list."`
	AutocalibrateInts []int
	Devices           string `short:"D" long:"devices" description:"Single device ID or a comma separated list of device IDs to use."`
	DeviceIDs         []int
	Intensity         string `short:"i" long:"intensity" description:"Intensities (the work size is 2^intensity) per device. Single global value or a comma separated list."`
	IntensityInts     []int
	TempTarget        string `short:"t" long:"temptarget" description:"Target temperature in Celsius to maintain via automatic fan control. (Requires --experimental flag)"`
	TempTargetInts    []uint32
	WorkSize          string `short:"W" long:"worksize" description:"The explicitly declared sizes of the work to do per device (overrides intensity). Single global value or a comma separated list."`
	WorkSizeInts      []uint32

	// Pool related options
	Pool         string `short:"o" long:"pool" description:"Pool to connect to (e.g.stratum+tcp://pool:port)"`
	PoolUser     string `short:"m" long:"pooluser" description:"Pool username"`
	PoolPassword string `short:"n" long:"poolpass" default-mask:"-" description:"Pool password"`
}

// removeDuplicateAddresses returns a new slice with all duplicate entries in
// addrs removed.
func removeDuplicateAddresses(addrs []string) []string {
	result := make([]string, 0, len(addrs))
	seen := map[string]struct{}{}
	for _, val := range addrs {
		if _, ok := seen[val]; !ok {
			result = append(result, val)
			seen[val] = struct{}{}
		}
	}
	return result
}

// normalizeAddress returns addr with the passed default port appended if
// there is not already a port specified.
func normalizeAddress(addr string, defaultPort string) string {
	_, _, err := net.SplitHostPort(addr)
	if err != nil {
		return net.JoinHostPort(addr, defaultPort)
	}
	return addr
}

// normalizeAddresses returns a new slice with all the passed peer addresses
// normalized with the given default port, and all duplicates removed.
func normalizeAddresses(addrs []string, defaultPort string) []string {
	for i, addr := range addrs {
		addrs[i] = normalizeAddress(addr, defaultPort)
	}

	return removeDuplicateAddresses(addrs)
}

// filesExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// validLogLevel returns whether or not logLevel is a valid debug log level.
func validLogLevel(logLevel string) bool {
	_, ok := slog.LevelFromString(logLevel)
	return ok
}

// supportedSubsystems returns a sorted slice of the supported subsystems for
// logging purposes.
func supportedSubsystems() []string {
	// Convert the subsystemLoggers map keys to a slice.
	subsystems := make([]string, 0, len(subsystemLoggers))
	for subsysID := range subsystemLoggers {
		subsystems = append(subsystems, subsysID)
	}

	// Sort the subsytems for stable display.
	sort.Strings(subsystems)
	return subsystems
}

// parseAndSetDebugLevels attempts to parse the specified debug level and set
// the levels accordingly.  An appropriate error is returned if anything is
// invalid.
func parseAndSetDebugLevels(debugLevel string) error {
	// When the specified string doesn't have any delimters, treat it as
	// the log level for all subsystems.
	if !strings.Contains(debugLevel, ",") && !strings.Contains(debugLevel, "=") {
		// Validate debug log level.
		if !validLogLevel(debugLevel) {
			str := "The specified debug level [%v] is invalid"
			return fmt.Errorf(str, debugLevel)
		}

		// Change the logging level for all subsystems.
		setLogLevels(debugLevel)

		return nil
	}

	// Split the specified string into subsystem/level pairs while detecting
	// issues and update the log levels accordingly.
	for _, logLevelPair := range strings.Split(debugLevel, ",") {
		if !strings.Contains(logLevelPair, "=") {
			str := "The specified debug level contains an invalid " +
				"subsystem/level pair [%v]"
			return fmt.Errorf(str, logLevelPair)
		}

		// Extract the specified subsystem and log level.
		fields := strings.Split(logLevelPair, "=")
		subsysID, logLevel := fields[0], fields[1]

		// Validate subsystem.
		if _, exists := subsystemLoggers[subsysID]; !exists {
			str := "The specified subsystem [%v] is invalid -- " +
				"supported subsytems %v"
			return fmt.Errorf(str, subsysID, supportedSubsystems())
		}

		// Validate log level.
		if !validLogLevel(logLevel) {
			str := "The specified debug level [%v] is invalid"
			return fmt.Errorf(str, logLevel)
		}

		setLogLevel(subsysID, logLevel)
	}

	return nil
}

// cleanAndExpandPath expands environement variables and leading ~ in the
// passed path, cleans the result, and returns it.
func cleanAndExpandPath(path string) string {
	// Expand initial ~ to OS specific home directory.
	if strings.HasPrefix(path, "~") {
		homeDir := filepath.Dir(minerHomeDir)
		path = strings.Replace(path, "~", homeDir, 1)
	}

	// NOTE: The os.ExpandEnv doesn't work with Windows-style %VARIABLE%,
	// but they variables can still be expanded via POSIX-style $VARIABLE.
	return filepath.Clean(os.ExpandEnv(path))
}

// loadConfig initializes and parses the config using a config file and command
// line options.
//
// The configuration proceeds as follows:
// 	1) Start with a default config with sane settings
// 	2) Pre-parse the command line to check for an alternative config file
// 	3) Load configuration file overwriting defaults with any specified options
// 	4) Parse CLI options and overwrite/add any specified options
//
// The above results in btcd functioning properly without any config settings
// while still allowing the user to override settings with config files and
// command line options.  Command line options always take precedence.
func loadConfig() (*config, []string, error) {
	// Default config.
	cfg := config{
		ConfigFile: defaultConfigFile,
		DebugLevel: defaultLogLevel,
		LogDir:     defaultLogDir,
		RPCServer:  defaultRPCServer,
		RPCCert:    defaultRPCCertFile,
		ClKernel:   defaultClKernel,
	}

	// Create the home directory if it doesn't already exist.
	funcName := "loadConfig"
	err := os.MkdirAll(minerHomeDir, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(-1)
	}

	// Pre-parse the command line options to see if an alternative config
	// file or the version flag was specified.
	preCfg := cfg
	preParser := flags.NewParser(&preCfg, flags.Default)
	_, err = preParser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			preParser.WriteHelp(os.Stderr)
		}
		return nil, nil, err
	}

	// Show the version and exit if the version flag was specified.
	appName := filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	usageMessage := fmt.Sprintf("Use %s -h to show usage", appName)
	if preCfg.ListDevices {
		ListDevices()
		os.Exit(0)
	}

	if preCfg.ShowVersion {
		fmt.Printf("%s %s version %s (Go version %s)\n", appName, gpuLib(), version(), runtime.Version())
		os.Exit(0)
	}

	// Load additional config from file.
	var configFileError error
	parser := flags.NewParser(&cfg, flags.Default)
	err = flags.NewIniParser(parser).ParseFile(preCfg.ConfigFile)
	if err != nil {
		if _, ok := err.(*os.PathError); !ok {
			fmt.Fprintln(os.Stderr, err)
			parser.WriteHelp(os.Stderr)
			return nil, nil, err
		}
		configFileError = err
	}

	// Parse command line options again to ensure they take precedence.
	remainingArgs, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		return nil, nil, err
	}

	// Multiple networks can't be selected simultaneously.
	numNets := 0
	if cfg.TestNet {
		numNets++
	}
	if cfg.SimNet {
		numNets++
	}
	if numNets > 1 {
		str := "%s: The testnet and simnet params can't be used " +
			"together -- choose one of the two"
		err := fmt.Errorf(str, "loadConfig")
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	// Check the autocalibrations if the user is setting that.
	if len(cfg.Autocalibrate) > 0 {
		// Parse a list like -A 450,600
		if strings.Contains(cfg.Autocalibrate, ",") {
			specifiedAutocalibrates := strings.Split(cfg.Autocalibrate, ",")
			cfg.AutocalibrateInts = make([]int, len(specifiedAutocalibrates))
			for i := range specifiedAutocalibrates {
				j, err := strconv.Atoi(specifiedAutocalibrates[i])
				if err != nil {
					err := fmt.Errorf("Could not convert autocalibration "+
						"(%v) to int: %s", specifiedAutocalibrates[i],
						err.Error())
					fmt.Fprintln(os.Stderr, err)
					return nil, nil, err
				}

				cfg.AutocalibrateInts[i] = j
			}
			// Use specified device like -A 600
		} else {
			cfg.AutocalibrateInts = make([]int, 1)
			i, err := strconv.Atoi(cfg.Autocalibrate)
			if err != nil {
				err := fmt.Errorf("Could not convert autocalibration %v "+
					"to int: %s", cfg.Autocalibrate, err.Error())
				fmt.Fprintln(os.Stderr, err)
				return nil, nil, err
			}

			cfg.AutocalibrateInts[0] = i
		}
		// Apply default
	} else {
		cfg.AutocalibrateInts = []int{defaultAutocalibrate}
	}

	// Check the devices if the user is setting that.
	if len(cfg.Devices) > 0 {
		// Parse a list like -D 1,2
		if strings.Contains(cfg.Devices, ",") {
			specifiedDevices := strings.Split(cfg.Devices, ",")
			cfg.DeviceIDs = make([]int, len(specifiedDevices))
			for i := range specifiedDevices {
				j, err := strconv.Atoi(specifiedDevices[i])
				if err != nil {
					err := fmt.Errorf("Could not convert device number %v "+
						"(%v) to int: %s", i+1, specifiedDevices[i],
						err.Error())
					fmt.Fprintln(os.Stderr, err)
					return nil, nil, err
				}

				cfg.DeviceIDs[i] = j
			}
			// Use specified device like -D 1
		} else {
			cfg.DeviceIDs = make([]int, 1)
			i, err := strconv.Atoi(cfg.Devices)
			if err != nil {
				err := fmt.Errorf("Could not convert specified device %v "+
					"to int: %s", cfg.Devices, err.Error())
				fmt.Fprintln(os.Stderr, err)
				return nil, nil, err
			}

			cfg.DeviceIDs[0] = i
		}
	}

	// Check the intensity if the user is setting that.
	if len(cfg.Intensity) > 0 {
		// Parse a list like -i 29,30
		if strings.Contains(cfg.Intensity, ",") {
			specifiedIntensities := strings.Split(cfg.Intensity, ",")
			cfg.IntensityInts = make([]int, len(specifiedIntensities))
			for i := range specifiedIntensities {
				j, err := strconv.Atoi(specifiedIntensities[i])
				if err != nil {
					err := fmt.Errorf("Could not convert intensity "+
						"(%v) to int: %s", specifiedIntensities[i],
						err.Error())
					fmt.Fprintln(os.Stderr, err)
					return nil, nil, err
				}

				cfg.IntensityInts[i] = j
			}
			// Use specified intensity like -i 29
		} else {
			cfg.IntensityInts = make([]int, 1)
			i, err := strconv.Atoi(cfg.Intensity)
			if err != nil {
				err := fmt.Errorf("Could not convert intensity %v "+
					"to int: %s", cfg.Intensity, err.Error())
				fmt.Fprintln(os.Stderr, err)
				return nil, nil, err
			}

			cfg.IntensityInts[0] = i
		}
	}

	for i := range cfg.IntensityInts {
		if (cfg.IntensityInts[i] < minIntensity) ||
			(cfg.IntensityInts[i] > maxIntensity) {
			err := fmt.Errorf("Intensity %v not within "+
				"range %v to %v.", cfg.IntensityInts[i], minIntensity,
				maxIntensity)
			fmt.Fprintln(os.Stderr, err)
			return nil, nil, err
		}
	}

	// Check the temptarget if the user is setting that.
	if len(cfg.TempTarget) > 0 {
		if !cfg.Experimental {
			err := fmt.Errorf("temperature targets / automatic fan control " +
				"is an EXPERIMENTAL feature and requires the --experimental " +
				"flag to acknowledge that you accept the risk of possibly " +
				"DAMAGING YOUR DEVICE(S) due to software bugs")
			fmt.Fprintln(os.Stderr, err)
			return nil, nil, err
		}
		// Parse a list like -t 80,75
		if strings.Contains(cfg.TempTarget, ",") {
			specifiedTempTargets := strings.Split(cfg.TempTarget, ",")
			cfg.TempTargetInts = make([]uint32, len(specifiedTempTargets))
			for i := range specifiedTempTargets {
				j, err := strconv.Atoi(specifiedTempTargets[i])
				if err != nil {
					err := fmt.Errorf("Could not convert temptarget "+
						"(%v) to int: %s", specifiedTempTargets[i],
						err.Error())
					fmt.Fprintln(os.Stderr, err)
					return nil, nil, err
				}

				cfg.TempTargetInts[i] = uint32(j)
			}
			// Use specified temptarget like -t 75
		} else {
			cfg.TempTargetInts = make([]uint32, 1)
			i, err := strconv.Atoi(cfg.TempTarget)
			if err != nil {
				err := fmt.Errorf("Could not convert temptarget %v "+
					"to int: %s", cfg.TempTarget, err.Error())
				fmt.Fprintln(os.Stderr, err)
				return nil, nil, err
			}

			cfg.TempTargetInts[0] = uint32(i)
		}
	}

	if cfg.Experimental {
		fmt.Fprintln(os.Stderr, "enabling EXPERIMENTAL features "+
			"that may possibly DAMAGE YOUR DEVICE(S)")
		time.Sleep(time.Second * 3)
	}

	for i := range cfg.TempTargetInts {
		if cfg.TempTargetInts[i] < minTempTarget {
			err := fmt.Errorf("Temp target %v is lower than minimum %v",
				cfg.TempTargetInts[i], minTempTarget)
			fmt.Fprintln(os.Stderr, err)
			return nil, nil, err
		}
		if cfg.TempTargetInts[i] > maxTempTarget {
			err := fmt.Errorf("Temp target %v is higher than maximum %v",
				cfg.TempTargetInts[i], maxTempTarget)
			fmt.Fprintln(os.Stderr, err)
			return nil, nil, err
		}
	}

	// Check the work size if the user is setting that.
	if len(cfg.WorkSize) > 0 {
		// Parse a list like -W 536870912,1073741824
		if strings.Contains(cfg.WorkSize, ",") {
			specifiedWorkSizes := strings.Split(cfg.WorkSize, ",")
			cfg.WorkSizeInts = make([]uint32, len(specifiedWorkSizes))
			for i := range specifiedWorkSizes {
				j, err := strconv.Atoi(specifiedWorkSizes[i])
				if err != nil {
					err := fmt.Errorf("Could not convert worksize "+
						"(%v) to int: %s", specifiedWorkSizes[i],
						err.Error())
					fmt.Fprintln(os.Stderr, err)
					return nil, nil, err
				}

				cfg.WorkSizeInts[i] = uint32(j)
			}
			// Use specified worksize like -W 1073741824
		} else {
			cfg.WorkSizeInts = make([]uint32, 1)
			i, err := strconv.Atoi(cfg.WorkSize)
			if err != nil {
				err := fmt.Errorf("Could not convert worksize %v "+
					"to int: %s", cfg.WorkSize, err.Error())
				fmt.Fprintln(os.Stderr, err)
				return nil, nil, err
			}

			cfg.WorkSizeInts[0] = uint32(i)
		}
	}

	for i := range cfg.WorkSizeInts {
		if cfg.WorkSizeInts[i] < 256 {
			err := fmt.Errorf("Too small WorkSize passed: %v, min 256",
				cfg.WorkSizeInts[i])
			fmt.Fprintln(os.Stderr, err)
			return nil, nil, err
		}
		if cfg.WorkSizeInts[i] > maxWorkSize {
			err := fmt.Errorf("Too big WorkSize passed: %v, max %v",
				cfg.WorkSizeInts[i], maxWorkSize)
			fmt.Fprintln(os.Stderr, err)
			return nil, nil, err
		}
		if cfg.WorkSizeInts[i]%256 != 0 {
			err := fmt.Errorf("Work size %v not a multiple of 256",
				cfg.WorkSizeInts[i])
			fmt.Fprintln(os.Stderr, err)
			return nil, nil, err
		}
	}

	// Special show command to list supported subsystems and exit.
	if cfg.DebugLevel == "show" {
		fmt.Println("Supported subsystems", supportedSubsystems())
		os.Exit(0)
	}

	// Initialize log rotation.  After log rotation has been initialized,
	// the logger variables may be used.
	initLogRotator(filepath.Join(cfg.LogDir, defaultLogFilename))

	// Parse, validate, and set debug log level(s).
	if err := parseAndSetDebugLevels(cfg.DebugLevel); err != nil {
		err := fmt.Errorf("%s: %v", funcName, err.Error())
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	if len(cfg.APIListeners) != 0 {
		cfg.APIListeners = normalizeAddresses(cfg.APIListeners, defaultAPIPort)
	}

	// Handle environment variable expansion in the RPC certificate path.
	cfg.RPCCert = cleanAndExpandPath(cfg.RPCCert)

	var defaultRPCPort string
	switch {
	case cfg.TestNet:
		defaultRPCPort = defaultRPCPortTestNet
		chainParams = chaincfg.TestNet3Params()
	case cfg.SimNet:
		defaultRPCPort = defaultRPCPortSimNet
		chainParams = chaincfg.SimNetParams()
	default:
		defaultRPCPort = defaultRPCPortMainNet
	}

	// Add default port to RPC server based on --testnet flag
	// if needed.
	cfg.RPCServer = normalizeAddress(cfg.RPCServer, defaultRPCPort)

	// Warn about missing config file only after all other configuration is
	// done.  This prevents the warning on help messages and invalid
	// options.  Note this should go directly before the return.
	if configFileError != nil {
		mainLog.Warnf("%v", configFileError)
	}

	return &cfg, remainingArgs, nil
}
