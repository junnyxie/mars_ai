package yahoo

import (
	"math"
	"testing"
)

func TestBuildMacroMarketItemRepairsKospiZeroVolumePlaceholderRise(t *testing.T) {
	quote := dailyQuote{
		Close: []*float64{
			floatPtr(8123.6201171875),
			floatPtr(8545.98046875),
			floatPtr(8726.599609375),
			floatPtr(8864.240234375),
			floatPtr(9063.83984375),
			floatPtr(9052.419921875),
			floatPtr(9114.5498046875),
			floatPtr(8203.83984375),
			floatPtr(8471.01953125),
			floatPtr(8838.73046875),
		},
		High: []*float64{
			floatPtr(8434.400390625),
			floatPtr(8603.48046875),
			floatPtr(8753.8203125),
			floatPtr(8872.1796875),
			floatPtr(9106.0703125),
			floatPtr(9385.58984375),
			floatPtr(9253.0),
			floatPtr(9175.4501953125),
			floatPtr(8471.01953125),
			floatPtr(8982.2197265625),
		},
		Low: []*float64{
			floatPtr(8079.77001953125),
			floatPtr(8450.240234375),
			floatPtr(8540.41015625),
			floatPtr(8605.66015625),
			floatPtr(8867.33984375),
			floatPtr(8831.7197265625),
			floatPtr(8900.6796875),
			floatPtr(8203.83984375),
			floatPtr(8471.01953125),
			floatPtr(8693.6201171875),
		},
		Volume: []*int64{
			int64Ptr(493400),
			int64Ptr(516600),
			int64Ptr(586300),
			int64Ptr(571200),
			int64Ptr(510900),
			int64Ptr(517200),
			int64Ptr(381100),
			int64Ptr(488700),
			int64Ptr(0),
			int64Ptr(156820),
		},
	}
	timestamps := []int64{1781222400, 1781481600, 1781568000, 1781654400, 1781740800, 1781827200, 1782086400, 1782172800, 1782259200, 1782345600}

	placeholderItem, err := buildMacroMarketItem(macroMarketSymbol{Symbol: "^KS11"}, quote, timestamps, 8)
	if err != nil {
		t.Fatalf("build placeholder item: %v", err)
	}
	if math.Abs(placeholderItem.Rise-(-7.0605)) > 0.0001 {
		t.Fatalf("placeholder rise = %.4f, want -7.0605", placeholderItem.Rise)
	}

	currentItem, err := buildMacroMarketItem(macroMarketSymbol{Symbol: "^KS11"}, quote, timestamps, 9)
	if err != nil {
		t.Fatalf("build current item: %v", err)
	}
	if math.Abs(currentItem.Rise-4.3408) > 0.0001 {
		t.Fatalf("current rise = %.4f, want 4.3408", currentItem.Rise)
	}
}

func floatPtr(value float64) *float64 {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}
