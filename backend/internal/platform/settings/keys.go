package settings

// Keys from docs/plan/01-product-rules.md §17 (D-61). TestKeysMatchMigration
// keeps this list, the seed migrations and 01 §17 identical.
//
// #nosec G101 -- these are setting key names, not credentials (D-19).
const (
	// Orders and strikes
	OrderAcceptTimeoutMin        Key = "order_accept_timeout_min"         // minutes, default 10
	PickupHoldMin                Key = "pickup_hold_min"                  // minutes, default 30
	PickupExtensionMin           Key = "pickup_extension_min"             // minutes, default 10
	PartialConfirmTimeoutMin     Key = "partial_confirm_timeout_min"      // minutes, default 5
	DeliveryDispatchTimeoutMin   Key = "delivery_dispatch_timeout_min"    // minutes, default 60
	NoshowStrikes                Key = "noshow_strikes"                   // count, default 3
	NoshowWindowDays             Key = "noshow_window_days"               // days, default 30
	NoshowLimitDays              Key = "noshow_limit_days"                // days, default 14
	ShopExpiryPenaltyThresholdBp Key = "shop_expiry_penalty_threshold_bp" // bp, default 3000
	StoreStrikeRiskThreshold     Key = "store_strike_risk_threshold"      // count, default 5
	// Stock and catalogue
	StockStaleAfterHours Key = "stock_stale_after_hours" // hours, default 72
	LowStockDefault      Key = "low_stock_default"       // count, default 5
	StoreBannersMax      Key = "store_banners_max"       // count, default 3
	// Credits and payouts
	CreditMaxPerOrder                 Key = "credit_max_per_order"                   // credits, default 30
	CreditMinOrderValuePaise          Key = "credit_min_order_value_paise"           // paise, default 10000
	CreditExpiryDays                  Key = "credit_expiry_days"                     // days, default 365
	CreditOrdersPerMonth              Key = "credit_orders_per_month"                // count, default 10
	StoreCreditRedemptionsPerDay      Key = "store_credit_redemptions_per_day"       // count, default 200
	AdminAdjustDualApprovalAbovePaise Key = "admin_adjust_dual_approval_above_paise" // paise, default 50000
	// Referrals
	RefRetailerCustomerReferrerCredits Key = "ref_retailer_customer_referrer_credits" // credits, default 30
	RefRetailerCustomerRefereeCredits  Key = "ref_retailer_customer_referee_credits"  // credits, default 100
	RefCustomerCustomerReferrerCredits Key = "ref_customer_customer_referrer_credits" // credits, default 30
	RefCustomerCustomerRefereeCredits  Key = "ref_customer_customer_referee_credits"  // credits, default 100
	RefAnyShopReferrerCredits          Key = "ref_any_shop_referrer_credits"          // credits, default 30
	RefAnyShopRefereeCredits           Key = "ref_any_shop_referee_credits"           // credits, default 100
	RefQualifyingMinOrderPaise         Key = "ref_qualifying_min_order_paise"         // paise, default 10000
	RefShopQualifyingOrders            Key = "ref_shop_qualifying_orders"             // count, default 10
	RefAttributionWindowDays           Key = "ref_attribution_window_days"            // days, default 7
	// POS
	PosReversalWindowHours    Key = "pos_reversal_window_hours"      // hours, default 24
	PosOfflineLinkMaxHours    Key = "pos_offline_link_max_hours"     // hours, default 12
	PosOfflineLinksRiskPerDay Key = "pos_offline_links_risk_per_day" // count, default 20
	// Search ranking
	SearchWText        Key = "search_w_text"        // bp, default 4500
	SearchWDistance    Key = "search_w_distance"    // bp, default 3000
	SearchWReliability Key = "search_w_reliability" // bp, default 1000
	SearchWRating      Key = "search_w_rating"      // bp, default 1000
	SearchWOffer       Key = "search_w_offer"       // bp, default 500
	// Demand insights and coach
	InsightNoSaleDays          Key = "insight_no_sale_days"           // days, default 21
	InsightMinDaysListed       Key = "insight_min_days_listed"        // days, default 21
	InsightExpiringWithinDays  Key = "insight_expiring_within_days"   // days, default 5
	InsightWantedNearbyRadiusM Key = "insight_wanted_nearby_radius_m" // meters, default 2000
	InsightMinSearches         Key = "insight_min_searches"           // count, default 10
	InsightMinSalesLines       Key = "insight_min_sales_lines"        // count, default 20
	InsightMinUnitsSellingFast Key = "insight_min_units_selling_fast" // count, default 5
	InsightMinEventsForPercent Key = "insight_min_events_for_percent" // count, default 20
	InsightPeriodDays          Key = "insight_period_days"            // days, default 7
	CoachMaxCardsPerDay        Key = "coach_max_cards_per_day"        // count, default 5
	CoachMaxPushPerDay         Key = "coach_max_push_per_day"         // count, default 1
	CoachDismissQuietDays      Key = "coach_dismiss_quiet_days"       // days, default 7
	// Field agents
	FieldAgentEditWindowDays Key = "field_agent_edit_window_days" // days, default 30
	// Feature flags
	FeatureDeliveryEnabled Key = "feature_delivery_enabled" // bool, default true
	FeatureBillQrEnabled   Key = "feature_bill_qr_enabled"  // bool, default true
	FeatureVoiceEnabled    Key = "feature_voice_enabled"    // bool, default true
	FeatureOffersEnabled   Key = "feature_offers_enabled"   // bool, default true
	FeatureBannersEnabled  Key = "feature_banners_enabled"  // bool, default true
	NotifyPushEnabled      Key = "notify_push_enabled"      // bool, default true
	NotifySmsEnabled       Key = "notify_sms_enabled"       // bool, default true
	NotifyWhatsappEnabled  Key = "notify_whatsapp_enabled"  // bool, default false
	// AI
	AiVoiceStockEnabled  Key = "ai_voice_stock_enabled"  // bool, default false
	AiBillscanEnabled    Key = "ai_billscan_enabled"     // bool, default false
	AiShelfscanEnabled   Key = "ai_shelfscan_enabled"    // bool, default false
	AiInsighttextEnabled Key = "ai_insighttext_enabled"  // bool, default false
	AiCoachEnabled       Key = "ai_coach_enabled"        // bool, default false
	AiReplydraftEnabled  Key = "ai_replydraft_enabled"   // bool, default false
	AiBannercopyEnabled  Key = "ai_bannercopy_enabled"   // bool, default false
	AiImagecheckEnabled  Key = "ai_imagecheck_enabled"   // bool, default false
	AiListparseEnabled   Key = "ai_listparse_enabled"    // bool, default false
	AiQuerynormEnabled   Key = "ai_querynorm_enabled"    // bool, default false
	AiCatalogueEnabled   Key = "ai_catalogue_enabled"    // bool, default false
	AiTriageEnabled      Key = "ai_triage_enabled"       // bool, default false
	AiRiskexplainEnabled Key = "ai_riskexplain_enabled"  // bool, default false
	AiModelFast          Key = "ai_model_fast"           // text, default claude-haiku-5-5
	AiModelSmart         Key = "ai_model_smart"          // text, default claude-sonnet-5-5
	AiDailyCallsStore    Key = "ai_daily_calls_store"    // count, default 100
	AiDailyCallsCustomer Key = "ai_daily_calls_customer" // count, default 30
	AiDailyCostCapPaise  Key = "ai_daily_cost_cap_paise" // paise, default 50000
	// Client versions
	MinClientVersionCustomer Key = "min_client_version_customer" // text, default 0.0.0
	MinClientVersionBusiness Key = "min_client_version_business" // text, default 0.0.0
	MinClientVersionAdmin    Key = "min_client_version_admin"    // text, default 0.0.0

	// Added after W0-03 (D-91–D-95; seeded by 00010_w0-03_settings_d91_d95.sql)
	OrderUnseenSmsAfterMin      Key = "order_unseen_sms_after_min"       // minutes, default 3
	PickupHoldReminderMin       Key = "pickup_hold_reminder_min"         // minutes, default 10
	RiskCodeLockoutsStorePerDay Key = "risk_code_lockouts_store_per_day" // count, default 10
	CreditExpiryReminderDays    Key = "credit_expiry_reminder_days"      // days, default 7
	ReviewEditWindowDays        Key = "review_edit_window_days"          // days, default 7
	JobRunsRetentionDays        Key = "job_runs_retention_days"          // days, default 90

	// Added by Demand insights v2 (D-99; seeded by 00011_w0-03_settings_d99.sql)
	InsightCoverDays                 Key = "insight_cover_days"                   // days, default 7
	InsightMissedSalesMinZeroDays    Key = "insight_missed_sales_min_zero_days"   // days, default 1
	InsightMissedSalesMinSearches    Key = "insight_missed_sales_min_searches"    // count, default 10
	InsightPairMinBills              Key = "insight_pair_min_bills"               // count, default 20
	InsightPairWindowDays            Key = "insight_pair_window_days"             // days, default 60
	InsightPairMinShareBp            Key = "insight_pair_min_share_bp"            // bp, default 2000
	InsightPairMinLiftPct            Key = "insight_pair_min_lift_pct"            // count, default 150
	InsightRegularMinVisits          Key = "insight_regular_min_visits"           // count, default 3
	InsightRegularWindowDays         Key = "insight_regular_window_days"          // days, default 60
	InsightRegularLapseDays          Key = "insight_regular_lapse_days"           // days, default 21
	InsightRegularMinLinkedCustomers Key = "insight_regular_min_linked_customers" // count, default 10
	InsightPriceCompareEnabled       Key = "insight_price_compare_enabled"        // bool, default false

	// Added by Ask shops nearby and search proof (D-101; seeded by 00012_w0-03_settings_d101.sql)
	AskDailyLimitCustomer   Key = "ask_daily_limit_customer"   // count, default 5
	AskRadiusM              Key = "ask_radius_m"               // meters, default 3000
	AskMaxStores            Key = "ask_max_stores"             // count, default 8
	AskDailyLimitStore      Key = "ask_daily_limit_store"      // count, default 40
	AskReplyWindowMin       Key = "ask_reply_window_min"       // minutes, default 15
	AskPriceValidMin        Key = "ask_price_valid_min"        // minutes, default 60
	AskLowYieldAfterCount   Key = "ask_low_yield_after_count"  // count, default 20
	AskLowYieldMaxStores    Key = "ask_low_yield_max_stores"   // count, default 3
	AskRetentionDays        Key = "ask_retention_days"         // days, default 90
	AskWhatsappEnabled      Key = "ask_whatsapp_enabled"       // bool, default false
	AskGapMinCustomers      Key = "ask_gap_min_customers"      // count, default 10
	AskGapWindowDays        Key = "ask_gap_window_days"        // days, default 7
	SiteMarqueeMinSearchers Key = "site_marquee_min_searchers" // count, default 10
	SiteMarqueeWindowDays   Key = "site_marquee_window_days"   // days, default 7
	SiteMarqueeMaxItems     Key = "site_marquee_max_items"     // count, default 20
	RefineAiMinWords        Key = "refine_ai_min_words"        // count, default 4
	AiVoicesearchEnabled    Key = "ai_voicesearch_enabled"     // bool, default false
	ShareCardCreatePerHour  Key = "share_card_create_per_hour" // count, default 30

	// Added by saved baskets (D-110; seeded by 21401_w2-14_baskets.sql)
	BasketMaxSaved      Key = "basket_max_saved"       // count, default 10
	BasketMaxItems      Key = "basket_max_items"       // count, default 30
	BasketSplitMaxShops Key = "basket_split_max_shops" // count, default 2
	BasketResultsMax    Key = "basket_results_max"     // count, default 10

	// Added by the POS counter-code locks (D-131; seeded by 20401_w2-04_pos.sql)
	PosCodePhoneFailuresPerHour Key = "pos_code_phone_failures_per_hour" // count, default 10

	// Added by the delivery returns (D-218; seeded by 20204_w2-02_return_reminder_settings.sql)
	DeliveryReturnFirstReminderHours  Key = "delivery_return_first_reminder_hours"  // hours, default 2
	DeliveryReturnSecondReminderHours Key = "delivery_return_second_reminder_hours" // hours, default 24

	// Added by the saved addresses (D-222; seeded by 21501_w2-15_saved_address_cap.sql)
	CustomerMaxSavedAddresses Key = "customer_max_saved_addresses" // count, default 20

	// Added by support tickets (D-226; seeded by 30801_w3-08_support.sql)
	SupportSlaUrgentMin Key = "support_sla_urgent_min" // minutes, default 60
	SupportSlaHighMin   Key = "support_sla_high_min"   // minutes, default 240
	SupportSlaNormalMin Key = "support_sla_normal_min" // minutes, default 1440
)

// AllKeys lists every key: the W0-03 keys in 01 §17 order, then keys added later (D-91–D-95, D-99, D-101, D-110, D-131, D-218, D-222, D-226).
var AllKeys = []Key{
	OrderAcceptTimeoutMin,
	PickupHoldMin,
	PickupExtensionMin,
	PartialConfirmTimeoutMin,
	DeliveryDispatchTimeoutMin,
	NoshowStrikes,
	NoshowWindowDays,
	NoshowLimitDays,
	ShopExpiryPenaltyThresholdBp,
	StoreStrikeRiskThreshold,
	StockStaleAfterHours,
	LowStockDefault,
	StoreBannersMax,
	CreditMaxPerOrder,
	CreditMinOrderValuePaise,
	CreditExpiryDays,
	CreditOrdersPerMonth,
	StoreCreditRedemptionsPerDay,
	AdminAdjustDualApprovalAbovePaise,
	RefRetailerCustomerReferrerCredits,
	RefRetailerCustomerRefereeCredits,
	RefCustomerCustomerReferrerCredits,
	RefCustomerCustomerRefereeCredits,
	RefAnyShopReferrerCredits,
	RefAnyShopRefereeCredits,
	RefQualifyingMinOrderPaise,
	RefShopQualifyingOrders,
	RefAttributionWindowDays,
	PosReversalWindowHours,
	PosOfflineLinkMaxHours,
	PosOfflineLinksRiskPerDay,
	SearchWText,
	SearchWDistance,
	SearchWReliability,
	SearchWRating,
	SearchWOffer,
	InsightNoSaleDays,
	InsightMinDaysListed,
	InsightExpiringWithinDays,
	InsightWantedNearbyRadiusM,
	InsightMinSearches,
	InsightMinSalesLines,
	InsightMinUnitsSellingFast,
	InsightMinEventsForPercent,
	InsightPeriodDays,
	CoachMaxCardsPerDay,
	CoachMaxPushPerDay,
	CoachDismissQuietDays,
	FieldAgentEditWindowDays,
	FeatureDeliveryEnabled,
	FeatureBillQrEnabled,
	FeatureVoiceEnabled,
	FeatureOffersEnabled,
	FeatureBannersEnabled,
	NotifyPushEnabled,
	NotifySmsEnabled,
	NotifyWhatsappEnabled,
	AiVoiceStockEnabled,
	AiBillscanEnabled,
	AiShelfscanEnabled,
	AiInsighttextEnabled,
	AiCoachEnabled,
	AiReplydraftEnabled,
	AiBannercopyEnabled,
	AiImagecheckEnabled,
	AiListparseEnabled,
	AiQuerynormEnabled,
	AiCatalogueEnabled,
	AiTriageEnabled,
	AiRiskexplainEnabled,
	AiModelFast,
	AiModelSmart,
	AiDailyCallsStore,
	AiDailyCallsCustomer,
	AiDailyCostCapPaise,
	MinClientVersionCustomer,
	MinClientVersionBusiness,
	MinClientVersionAdmin,
	OrderUnseenSmsAfterMin,
	PickupHoldReminderMin,
	RiskCodeLockoutsStorePerDay,
	CreditExpiryReminderDays,
	ReviewEditWindowDays,
	JobRunsRetentionDays,
	InsightCoverDays,
	InsightMissedSalesMinZeroDays,
	InsightMissedSalesMinSearches,
	InsightPairMinBills,
	InsightPairWindowDays,
	InsightPairMinShareBp,
	InsightPairMinLiftPct,
	InsightRegularMinVisits,
	InsightRegularWindowDays,
	InsightRegularLapseDays,
	InsightRegularMinLinkedCustomers,
	InsightPriceCompareEnabled,
	AskDailyLimitCustomer,
	AskRadiusM,
	AskMaxStores,
	AskDailyLimitStore,
	AskReplyWindowMin,
	AskPriceValidMin,
	AskLowYieldAfterCount,
	AskLowYieldMaxStores,
	AskRetentionDays,
	AskWhatsappEnabled,
	AskGapMinCustomers,
	AskGapWindowDays,
	SiteMarqueeMinSearchers,
	SiteMarqueeWindowDays,
	SiteMarqueeMaxItems,
	RefineAiMinWords,
	AiVoicesearchEnabled,
	ShareCardCreatePerHour,
	BasketMaxSaved,
	BasketMaxItems,
	BasketSplitMaxShops,
	BasketResultsMax,
	PosCodePhoneFailuresPerHour,
	DeliveryReturnFirstReminderHours,
	DeliveryReturnSecondReminderHours,
	CustomerMaxSavedAddresses,
	SupportSlaUrgentMin,
	SupportSlaHighMin,
	SupportSlaNormalMin,
}
