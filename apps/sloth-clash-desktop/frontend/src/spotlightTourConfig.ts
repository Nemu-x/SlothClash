/** Tour step layout; copy keys resolved via i18n in `SpotlightTour`. */
export const SPOTLIGHT_TOUR_STEPS = [
  {
    selector: '[data-tour="connect"]',
    titleKey: 'tour.connectTitle',
    bodyKey: 'tour.connectBody',
  },
  {
    selector: '[data-tour="mode"]',
    titleKey: 'tour.modeTitle',
    bodyKey: 'tour.modeBody',
  },
  {
    selector: '[data-tour="traffic"]',
    titleKey: 'tour.trafficTitle',
    bodyKey: 'tour.trafficBody',
  },
  {
    selector: '[data-tour="service"]',
    titleKey: 'tour.serviceTitle',
    bodyKey: 'tour.serviceBody',
  },
] as const

export const SPOTLIGHT_TOUR_STEP_COUNT = SPOTLIGHT_TOUR_STEPS.length
