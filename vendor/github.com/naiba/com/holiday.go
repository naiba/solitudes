package com

import (
	"fmt"
	"time"
)

//HolidayOvertimeWork 法定加班
const HolidayOvertimeWork = 0

//HolidayWorkingDay 工作日
const HolidayWorkingDay = 1

//HolidayWeekend 周末
const HolidayWeekend = 2

//HolidayGov 法定节假日
const HolidayGov = 3

var govHolidayList = map[string]int{
	// 元旦
	"0101": HolidayGov,
	// 春节
	"0211": HolidayOvertimeWork,
	"0215": HolidayGov,
	"0216": HolidayGov,
	"0217": HolidayGov,
	"0218": HolidayGov,
	"0219": HolidayGov,
	"0220": HolidayGov,
	"0221": HolidayGov,
	"0224": HolidayOvertimeWork,
	// 清明节
	"0405": HolidayGov,
	"0406": HolidayGov,
	"0407": HolidayGov,
	"0408": HolidayOvertimeWork,
	// 劳动节
	"0428": HolidayOvertimeWork,
	"0429": HolidayGov,
	"0430": HolidayGov,
	"0501": HolidayGov,
	// 端午节
	"0618": HolidayGov,
	// 中秋节
	"0924": HolidayGov,
	// 国庆节
	"0929": HolidayOvertimeWork,
	"0930": HolidayOvertimeWork,
	"1001": HolidayGov,
	"1002": HolidayGov,
	"1003": HolidayGov,
	"1004": HolidayGov,
	"1005": HolidayGov,
	"1006": HolidayGov,
	"1007": HolidayGov,
}

//IsHoliday 中国法定节假日
func IsHoliday(t time.Time) int {
	mark := fmt.Sprintf(`%0.2d`, t.Month()) + fmt.Sprintf(`%0.2d`, t.Day())
	if is, has := govHolidayList[mark]; has {
		return is
	}
	if IsWeekend(t) {
		return HolidayWeekend
	}
	return HolidayWorkingDay
}

//IsWeekend 周末判断
func IsWeekend(t time.Time) bool {
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return true
	}
	return false
}
