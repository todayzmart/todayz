/** The earliest holiday that may be picked: today in India, `YYYY-MM-DD`. */
export const todayIst = (): string => new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Kolkata' }).format(new Date());
