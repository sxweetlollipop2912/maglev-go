package viper

import (
	"github.com/inhies/go-bytesize"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"
	"reflect"
)

// Unmarshal unmarshals the config into a Struct. Make sure that the tags
// on the fields of the structure are properly set.
// Unmarshal use
//
//	 mapstructure.ComposeDecodeHookFunc(
//			mapstructure.StringToTimeDurationHookFunc(),
//			StringToByteSizeHookFunc(),
//		)
//
// as default decode hook funcs.
func Unmarshal(v *viper.Viper, rawVal interface{}, opts ...viper.DecoderConfigOption) error {
	return v.Unmarshal(
		rawVal,
		append(
			opts,
			viper.DecodeHook(
				mapstructure.ComposeDecodeHookFunc(
					mapstructure.StringToTimeDurationHookFunc(),
					StringToByteSizeHookFunc(),
				),
			),
		)...,
	)
}

// StringToByteSizeHookFunc returns a DecodeHookFunc that converts
// hex string to bytesize.ByteSize.
func StringToByteSizeHookFunc() mapstructure.DecodeHookFunc {
	return func(
		f reflect.Type,
		t reflect.Type,
		data interface{},
	) (interface{}, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}

		if t != reflect.TypeOf(bytesize.B) {
			return data, nil
		}

		sDec, err := bytesize.Parse(data.(string))
		if err != nil {
			return nil, err
		}

		return sDec, nil
	}
}
