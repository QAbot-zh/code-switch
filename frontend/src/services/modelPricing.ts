import { Call } from '@wailsio/runtime'

export type PricingStatus = {
  source: string
  updated_at: string
  model_count: number
  fetched_url?: string
  imported_path?: string
  upstream_url: string
  override_path: string
}

const emptyStatus: PricingStatus = {
  source: 'builtin',
  updated_at: '',
  model_count: 0,
  upstream_url: '',
  override_path: '',
}

export const fetchPricingStatus = async (): Promise<PricingStatus> => {
  const data = await Call.ByName('codeswitch/services.PricingService.GetStatus')
  return (data as PricingStatus) ?? emptyStatus
}

export const updatePricingFromUpstream = async (): Promise<PricingStatus> => {
  const data = await Call.ByName('codeswitch/services.PricingService.UpdateFromUpstream')
  return data as PricingStatus
}

export const updatePricingFromFile = async (path: string): Promise<PricingStatus> => {
  const data = await Call.ByName('codeswitch/services.PricingService.UpdateFromFile', path)
  return data as PricingStatus
}

export const resetPricingToBuiltin = async (): Promise<PricingStatus> => {
  const data = await Call.ByName('codeswitch/services.PricingService.ResetToBuiltin')
  return data as PricingStatus
}
