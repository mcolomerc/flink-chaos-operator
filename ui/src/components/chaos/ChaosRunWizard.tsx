import { useState, useEffect, useCallback } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useAppStore } from '../../store'
import { createChaosRun } from '../../api/client'
import type { CreateChaosRunRequest } from '../../api/client'

// ── Types ──────────────────────────────────────────────────────────────────

type ScenarioType = 'TaskManagerPodKill' | 'NetworkPartition' | 'NetworkChaos' | 'ResourceExhaustion'

interface WizardForm {
  step: 1 | 2 | 3 | 4
  scenarioType: ScenarioType
  // Pod kill
  selectionCount: number
  gracePeriod: number
  // Network
  networkTarget: string
  networkDirection: string
  externalHostname: string
  externalCIDR: string
  externalPort: number
  latencyMs: number
  jitterMs: number
  lossPercent: number
  bandwidth: string
  durationSeconds: number
  // Resource exhaustion
  resourceMode: 'CPU' | 'Memory'
  workers: number
  memoryPercent: number
  // Safety
  minTaskManagers: number
  dryRun: boolean
}

interface WizardTarget {
  podName: string
  nodeType: 'jobManager' | 'taskManager'
  deploymentName: string
  targetType: string
  deploymentId?: string
  vvpNamespace?: string
  checkpointEndpoint?: string
}

const DEFAULT_FORM: WizardForm = {
  step: 1,
  scenarioType: 'TaskManagerPodKill',
  selectionCount: 1,
  gracePeriod: 0,
  networkTarget: 'TMtoJM',
  networkDirection: 'Both',
  externalHostname: '',
  externalCIDR: '',
  externalPort: 0,
  latencyMs: 100,
  jitterMs: 0,
  lossPercent: 0,
  bandwidth: '',
  durationSeconds: 60,
  resourceMode: 'CPU',
  workers: 1,
  memoryPercent: 80,
  minTaskManagers: 1,
  dryRun: false,
}

// ── YAML builder ──────────────────────────────────────────────────────────

function yamlStr(v: string): string {
  if (!v) return "''"
  if (/[:#\[\]{},|>&!'"@`]/.test(v) || v !== v.trim() || v.includes('\n')) {
    return `'${v.replace(/'/g, "''")}'`
  }
  return v
}

function buildYAML(target: WizardTarget, form: WizardForm): string {
  const targetLines = target.targetType === 'VervericaDeployment'
    ? `    type: VervericaDeployment\n    deploymentName: ${yamlStr(target.deploymentName)}${target.deploymentId ? `\n    deploymentId: ${yamlStr(target.deploymentId)}` : ''}${target.vvpNamespace ? `\n    vvpNamespace: ${yamlStr(target.vvpNamespace)}` : ''}`
    : `    type: FlinkDeployment\n    name: ${yamlStr(target.deploymentName)}`

  const base = `apiVersion: chaos.flink.io/v1alpha1
kind: ChaosRun
metadata:
  name: chaos-run-<preview>
  namespace: (current namespace)
spec:
  target:
${targetLines}
  scenario:
    type: ${form.scenarioType}`

  let scenarioSection = ''

  if (form.scenarioType === 'TaskManagerPodKill') {
    scenarioSection = `
    selection:
      mode: Random
      count: ${form.selectionCount}
    action:
      type: DeletePod
      gracePeriodSeconds: ${form.gracePeriod}`
  } else if (form.scenarioType === 'NetworkPartition') {
    const extLines = form.externalHostname || form.externalCIDR
      ? `\n      externalEndpoint:\n` +
        (form.externalHostname ? `        hostname: ${yamlStr(form.externalHostname)}\n` : '') +
        (form.externalCIDR ? `        cidr: ${yamlStr(form.externalCIDR)}\n` : '') +
        (form.externalPort > 0 ? `        port: ${form.externalPort}\n` : '')
      : ''
    scenarioSection = `
    network:
      target: ${form.networkTarget}
      direction: ${form.networkDirection}${extLines}      duration: ${form.durationSeconds}s`
  } else if (form.scenarioType === 'NetworkChaos') {
    const bwLine = form.bandwidth ? `\n      bandwidth: ${yamlStr(form.bandwidth)}` : ''
    const extLines = form.externalHostname || form.externalCIDR
      ? `\n      externalEndpoint:\n` +
        (form.externalHostname ? `        hostname: ${yamlStr(form.externalHostname)}\n` : '') +
        (form.externalCIDR ? `        cidr: ${yamlStr(form.externalCIDR)}\n` : '') +
        (form.externalPort > 0 ? `        port: ${form.externalPort}\n` : '')
      : ''
    scenarioSection = `
    network:
      target: ${form.networkTarget}
      direction: ${form.networkDirection}
      latency: ${form.latencyMs}ms
      jitter: ${form.jitterMs}ms
      loss: ${form.lossPercent}${bwLine}${extLines}      duration: ${form.durationSeconds}s`
  } else if (form.scenarioType === 'ResourceExhaustion') {
    const memLine = form.resourceMode === 'Memory' ? `\n      memoryPercent: ${form.memoryPercent}` : ''
    scenarioSection = `
    resourceExhaustion:
      mode: ${form.resourceMode}
      workers: ${form.workers}${memLine}
      durationSeconds: ${form.durationSeconds}`
  }

  const observeSection = form.scenarioType === 'TaskManagerPodKill' ? `
  observe:
    enabled: true
    timeout: ${form.durationSeconds}s` : ''

  const safety = `
  safety:
    minTaskManagers: ${form.minTaskManagers}
    dryRun: ${form.dryRun}`

  return base + scenarioSection + observeSection + safety
}

// ── Step 1: Scenario selection ─────────────────────────────────────────────

interface ScenarioCard {
  type: ScenarioType
  label: string
  description: string
  color: 'red' | 'orange' | 'yellow'
}

const SCENARIO_CARDS: ScenarioCard[] = [
  {
    type: 'TaskManagerPodKill',
    label: 'TaskManager Pod Kill',
    description: 'Terminate one or more TaskManager pods',
    color: 'red',
  },
  {
    type: 'NetworkPartition',
    label: 'Network Partition',
    description: 'Binary traffic block via NetworkPolicy',
    color: 'orange',
  },
  {
    type: 'NetworkChaos',
    label: 'Network Chaos',
    description: 'Latency / packet loss / bandwidth via tc netem',
    color: 'orange',
  },
  {
    type: 'ResourceExhaustion',
    label: 'Resource Exhaustion',
    description: 'CPU or memory stress via stress-ng',
    color: 'yellow',
  },
]

const selectedCardClass: Record<string, string> = {
  red: 'border-red-500 bg-red-950/30',
  orange: 'border-orange-500 bg-orange-950/30',
  yellow: 'border-yellow-500 bg-yellow-950/30',
}

function Step1ScenarioType({
  scenarioType,
  onChange,
}: {
  scenarioType: ScenarioType
  onChange: (s: ScenarioType) => void
}) {
  return (
    <div>
      <p className="text-slate-400 text-xs mb-4">Select the type of chaos to inject.</p>
      <div className="grid grid-cols-2 gap-3">
        {SCENARIO_CARDS.map((card) => {
          const isSelected = scenarioType === card.type
          const selectedCls = selectedCardClass[card.color]
          return (
            <button
              key={card.type}
              type="button"
              onClick={() => onChange(card.type)}
              className={[
                'cursor-pointer rounded-lg border-2 p-3 text-left transition-colors',
                isSelected
                  ? selectedCls
                  : 'border-slate-700 bg-slate-900/50 hover:border-slate-500',
              ].join(' ')}
            >
              <span className="block text-slate-100 text-sm font-bold mb-1">{card.label}</span>
              <span className="block text-slate-400 text-xs">{card.description}</span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

// ── Shared input primitives ────────────────────────────────────────────────

const inputCls =
  'bg-slate-900 border border-slate-600 rounded px-2 py-1.5 text-slate-100 text-sm w-full focus:outline-none focus:border-blue-500'
const labelCls = 'text-slate-400 text-xs mb-1 block'

function FieldGroup({
  label,
  htmlFor,
  children,
}: {
  label: string
  htmlFor?: string
  children: React.ReactNode
}) {
  return (
    <div>
      {htmlFor ? (
        <label htmlFor={htmlFor} className={labelCls}>{label}</label>
      ) : (
        <div role="group" aria-label={label} className={labelCls}>{label}</div>
      )}
      {children}
    </div>
  )
}

// ── Step 2: Scenario parameters ────────────────────────────────────────────

function Step2Params({
  form,
  setField,
  target,
}: {
  form: WizardForm
  setField: <K extends keyof WizardForm>(key: K, value: WizardForm[K]) => void
  target: WizardTarget
}) {
  const { scenarioType } = form

  const needsExternalEndpoint =
    form.networkTarget === 'TMtoCheckpoint' || form.networkTarget === 'TMtoExternal'

  const networkTargetOptions = [
    { value: 'TMtoJM', label: 'TM \u2194 JobManager' },
    { value: 'TMtoTM', label: 'TM \u2194 TM' },
    { value: 'TMtoCheckpoint', label: 'TM \u2194 Checkpoint storage' },
    { value: 'TMtoExternal', label: 'TM \u2194 External endpoint' },
  ]

  // When the user picks TMtoCheckpoint, auto-fill hostname from detected storage endpoint.
  function handleNetworkTargetChange(val: string) {
    setField('networkTarget', val)
    if (val === 'TMtoCheckpoint' && target.checkpointEndpoint && !form.externalHostname) {
      // Extract hostname from the storage URL (e.g. s3://bucket → bucket, hdfs://host:port → host)
      try {
        const url = new URL(target.checkpointEndpoint)
        setField('externalHostname', url.hostname || url.pathname.split('/')[0] || target.checkpointEndpoint)
      } catch {
        setField('externalHostname', target.checkpointEndpoint)
      }
    }
  }

  const networkDirectionOptions = ['Both', 'Ingress', 'Egress']

  return (
    <div className="space-y-4">
      {/* Scenario badge */}
      <div>
        <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-semibold bg-slate-700 text-slate-200 border border-slate-600">
          {scenarioType}
        </span>
      </div>

      {scenarioType === 'TaskManagerPodKill' && (
        <>
          <FieldGroup label="Pods to kill" htmlFor="wiz-selection-count">
            <input
              id="wiz-selection-count"
              type="number"
              min={1}
              max={20}
              value={form.selectionCount}
              onChange={(e) => setField('selectionCount', Math.max(1, Math.min(20, Number(e.target.value))))}
              className={inputCls}
            />
          </FieldGroup>
          <FieldGroup label="Grace period (seconds) — 0 = immediate" htmlFor="wiz-grace-period">
            <input
              id="wiz-grace-period"
              type="number"
              min={0}
              value={form.gracePeriod}
              onChange={(e) => setField('gracePeriod', Math.max(0, Number(e.target.value)))}
              className={inputCls}
            />
          </FieldGroup>
          <FieldGroup label="Recovery timeout (seconds) — how long to wait for the pod to restart" htmlFor="wiz-duration-podkill">
            <input
              id="wiz-duration-podkill"
              type="number"
              min={10}
              value={form.durationSeconds}
              onChange={(e) => setField('durationSeconds', Math.max(10, Number(e.target.value)))}
              className={inputCls}
            />
          </FieldGroup>
        </>
      )}

      {(scenarioType === 'NetworkPartition' || scenarioType === 'NetworkChaos') && (
        <>
          <FieldGroup label="Network target" htmlFor="wiz-network-target">
            <select
              id="wiz-network-target"
              value={form.networkTarget}
              onChange={(e) => handleNetworkTargetChange(e.target.value)}
              className={inputCls}
            >
              {networkTargetOptions.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </FieldGroup>

          {needsExternalEndpoint && (
            <>
              {scenarioType === 'NetworkPartition' && (
                <div className="flex gap-2 items-start rounded-lg border border-blue-700 bg-blue-950/30 px-3 py-2.5">
                  <span className="text-blue-400 text-sm flex-shrink-0" aria-hidden="true">ℹ</span>
                  <p className="text-blue-300 text-xs leading-relaxed">
                    <strong>NetworkPartition</strong> uses Kubernetes NetworkPolicy, which is IP-based.
                    A <strong>CIDR</strong> is required — hostnames cannot be used.
                    For hostname-based blocking (e.g. S3 endpoints) use <strong>NetworkChaos</strong> instead.
                  </p>
                </div>
              )}

              <FieldGroup label={`External CIDR${scenarioType === 'NetworkPartition' ? ' *required*' : ' (optional for NetworkChaos)'} — e.g. 52.216.0.0/15`} htmlFor="wiz-external-cidr">
                <input
                  id="wiz-external-cidr"
                  type="text"
                  value={form.externalCIDR}
                  onChange={(e) => setField('externalCIDR', e.target.value)}
                  placeholder="e.g. 52.216.0.0/15"
                  className={[
                    inputCls,
                    scenarioType === 'NetworkPartition' && needsExternalEndpoint && !form.externalCIDR
                      ? 'border-red-500 focus:border-red-400'
                      : '',
                  ].join(' ')}
                />
                {scenarioType === 'NetworkPartition' && needsExternalEndpoint && !form.externalCIDR && (
                  <p className="text-red-400 text-[10px] mt-1">CIDR is required for NetworkPartition</p>
                )}
              </FieldGroup>

              <FieldGroup label="External hostname (NetworkChaos only, e.g. s3.amazonaws.com)" htmlFor="wiz-external-hostname">
                <input
                  id="wiz-external-hostname"
                  type="text"
                  value={form.externalHostname}
                  onChange={(e) => setField('externalHostname', e.target.value)}
                  placeholder={
                    form.networkTarget === 'TMtoCheckpoint' && target.checkpointEndpoint
                      ? `auto-detected: ${target.checkpointEndpoint}`
                      : 'e.g. s3.amazonaws.com'
                  }
                  disabled={scenarioType === 'NetworkPartition'}
                  className={[inputCls, scenarioType === 'NetworkPartition' ? 'opacity-40 cursor-not-allowed' : ''].join(' ')}
                />
              </FieldGroup>

              <FieldGroup label="Port (optional, 0 = all ports)" htmlFor="wiz-external-port">
                <input
                  id="wiz-external-port"
                  type="number"
                  min={0}
                  max={65535}
                  value={form.externalPort}
                  onChange={(e) => setField('externalPort', Math.max(0, Math.min(65535, Number(e.target.value))))}
                  className={inputCls}
                />
              </FieldGroup>
            </>
          )}
          <FieldGroup label="Direction" htmlFor="wiz-direction">
            <select
              id="wiz-direction"
              value={form.networkDirection}
              onChange={(e) => setField('networkDirection', e.target.value)}
              className={inputCls}
            >
              {networkDirectionOptions.map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </select>
          </FieldGroup>
        </>
      )}

      {scenarioType === 'NetworkChaos' && (
        <>
          <FieldGroup label="Latency (ms)" htmlFor="wiz-latency">
            <input
              id="wiz-latency"
              type="number"
              min={0}
              value={form.latencyMs}
              onChange={(e) => setField('latencyMs', Math.max(0, Number(e.target.value)))}
              className={inputCls}
            />
          </FieldGroup>
          <FieldGroup label="Jitter (ms)" htmlFor="wiz-jitter">
            <input
              id="wiz-jitter"
              type="number"
              min={0}
              value={form.jitterMs}
              onChange={(e) => setField('jitterMs', Math.max(0, Number(e.target.value)))}
              className={inputCls}
            />
          </FieldGroup>
          <FieldGroup label="Packet loss (%)" htmlFor="wiz-loss">
            <input
              id="wiz-loss"
              type="number"
              min={0}
              max={100}
              value={form.lossPercent}
              onChange={(e) => setField('lossPercent', Math.max(0, Math.min(100, Number(e.target.value))))}
              className={inputCls}
            />
          </FieldGroup>
          <FieldGroup label="Bandwidth limit (e.g. 10mbit)" htmlFor="wiz-bandwidth">
            <input
              id="wiz-bandwidth"
              type="text"
              value={form.bandwidth}
              onChange={(e) => setField('bandwidth', e.target.value)}
              placeholder="leave blank for no limit"
              className={inputCls}
            />
          </FieldGroup>
        </>
      )}

      {(scenarioType === 'NetworkPartition' || scenarioType === 'NetworkChaos') && (
        <FieldGroup label="Duration (seconds)" htmlFor="wiz-duration-network">
          <input
            id="wiz-duration-network"
            type="number"
            min={1}
            value={form.durationSeconds}
            onChange={(e) => setField('durationSeconds', Math.max(1, Number(e.target.value)))}
            className={inputCls}
          />
        </FieldGroup>
      )}

      {scenarioType === 'ResourceExhaustion' && (
        <>
          <FieldGroup label="Mode">
            <div className="flex gap-4">
              {(['CPU', 'Memory'] as const).map((mode) => (
                <label key={mode} className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="radio"
                    name="resourceMode"
                    value={mode}
                    checked={form.resourceMode === mode}
                    onChange={() => setField('resourceMode', mode)}
                    className="accent-blue-500"
                  />
                  <span className="text-slate-200 text-sm">{mode}</span>
                </label>
              ))}
            </div>
          </FieldGroup>
          <FieldGroup label={form.resourceMode === 'Memory' ? 'VM workers — parallel stress-ng --vm processes, each allocating Memory %' : 'CPU workers — parallel stress-ng --cpu threads (1 = one full core saturated)'} htmlFor="wiz-workers">
            <input
              id="wiz-workers"
              type="number"
              min={1}
              value={form.workers}
              onChange={(e) => setField('workers', Math.max(1, Number(e.target.value)))}
              className={inputCls}
            />
          </FieldGroup>
          {form.resourceMode === 'Memory' && (
            <FieldGroup label="Memory %" htmlFor="wiz-memory-percent">
              <input
                id="wiz-memory-percent"
                type="number"
                min={1}
                max={100}
                value={form.memoryPercent}
                onChange={(e) => setField('memoryPercent', Math.max(1, Math.min(100, Number(e.target.value))))}
                className={inputCls}
              />
            </FieldGroup>
          )}
          <FieldGroup label="Duration (seconds)" htmlFor="wiz-duration-resource">
            <input
              id="wiz-duration-resource"
              type="number"
              min={1}
              value={form.durationSeconds}
              onChange={(e) => setField('durationSeconds', Math.max(1, Number(e.target.value)))}
              className={inputCls}
            />
          </FieldGroup>
        </>
      )}
    </div>
  )
}

// ── Step 3: Safety settings ────────────────────────────────────────────────

function Step3Safety({
  form,
  setField,
}: {
  form: WizardForm
  setField: <K extends keyof WizardForm>(key: K, value: WizardForm[K]) => void
}) {
  return (
    <div className="space-y-5">
      <FieldGroup label="Minimum TaskManagers to keep running" htmlFor="wiz-min-task-managers">
        <input
          id="wiz-min-task-managers"
          type="number"
          min={0}
          value={form.minTaskManagers}
          onChange={(e) => setField('minTaskManagers', Math.max(0, Number(e.target.value)))}
          className={inputCls}
        />
      </FieldGroup>

      <label className="flex items-start gap-3 cursor-pointer">
        <input
          type="checkbox"
          checked={form.dryRun}
          onChange={(e) => setField('dryRun', e.target.checked)}
          className="mt-0.5 accent-blue-500 w-4 h-4 flex-shrink-0"
        />
        <span className="text-slate-200 text-sm leading-snug">
          Dry run{' '}
          <span className="text-slate-400 text-xs font-normal">(preview only, no actual injection)</span>
        </span>
      </label>

      {!form.dryRun && (
        <div className="flex gap-2 items-start rounded-lg border border-yellow-700 bg-yellow-950/30 px-3 py-2.5">
          <span className="text-yellow-400 text-sm flex-shrink-0" aria-hidden="true">
            &#9888;
          </span>
          <p className="text-yellow-300 text-xs leading-relaxed">
            This will inject real chaos into your cluster. Confirm you have the appropriate access
            and that the target is a non-production workload.
          </p>
        </div>
      )}
    </div>
  )
}

// ── Step 4: YAML preview ───────────────────────────────────────────────────

function Step4Preview({
  target,
  form,
  errorMessage,
}: {
  target: WizardTarget
  form: WizardForm
  errorMessage?: string
}) {
  const yaml = buildYAML(target, form)

  return (
    <div className="space-y-4">
      <p className="text-slate-400 text-xs">
        Review the resource that will be created, then click Submit to launch the experiment.
      </p>

      <pre className="bg-slate-950 border border-slate-700 rounded-lg p-3 text-xs font-mono text-slate-300 overflow-x-auto whitespace-pre leading-relaxed">
        {yaml}
      </pre>

      {errorMessage && (
        <div className="rounded border border-red-700 bg-red-950/40 px-3 py-2 text-red-400 text-xs">
          {errorMessage}
        </div>
      )}
    </div>
  )
}

// ── Main wizard ────────────────────────────────────────────────────────────

function buildRequest(target: WizardTarget, form: WizardForm): CreateChaosRunRequest {
  const base: CreateChaosRunRequest = {
    targetName: target.deploymentName,
    targetType: target.targetType,
    deploymentId: target.deploymentId,
    vvpNamespace: target.vvpNamespace,
    scenarioType: form.scenarioType,
    selectionMode: 'Random',
    minTaskManagers: form.minTaskManagers,
    dryRun: form.dryRun,
  }

  if (form.scenarioType === 'TaskManagerPodKill') {
    base.selectionCount = form.selectionCount
    base.gracePeriodSeconds = form.gracePeriod
    base.durationSeconds = form.durationSeconds
  } else if (form.scenarioType === 'NetworkPartition') {
    base.networkTarget = form.networkTarget
    base.networkDirection = form.networkDirection
    base.durationSeconds = form.durationSeconds
    if (form.externalHostname) base.externalHostname = form.externalHostname
    if (form.externalCIDR) base.externalCIDR = form.externalCIDR
    if (form.externalPort > 0) base.externalPort = form.externalPort
  } else if (form.scenarioType === 'NetworkChaos') {
    base.networkTarget = form.networkTarget
    base.networkDirection = form.networkDirection
    base.latencyMs = form.latencyMs
    base.jitterMs = form.jitterMs
    base.lossPercent = form.lossPercent
    if (form.bandwidth) base.bandwidth = form.bandwidth
    base.durationSeconds = form.durationSeconds
    if (form.externalHostname) base.externalHostname = form.externalHostname
    if (form.externalCIDR) base.externalCIDR = form.externalCIDR
    if (form.externalPort > 0) base.externalPort = form.externalPort
  } else if (form.scenarioType === 'ResourceExhaustion') {
    base.resourceMode = form.resourceMode
    base.workers = form.workers
    if (form.resourceMode === 'Memory') base.memoryPercent = form.memoryPercent
    base.durationSeconds = form.durationSeconds
  }

  return base
}

export default function ChaosRunWizard() {
  const wizardTarget = useAppStore((s) => s.wizardTarget)
  const closeWizard = useAppStore((s) => s.closeWizard)
  const queryClient = useQueryClient()

  const [form, setForm] = useState<WizardForm>({ ...DEFAULT_FORM })

  // Reset form whenever wizard opens on a new target
  useEffect(() => {
    if (wizardTarget) {
      setForm({ ...DEFAULT_FORM })
    }
  }, [wizardTarget])

  // ESC key closes the wizard
  useEffect(() => {
    if (!wizardTarget) return
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') closeWizard()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [wizardTarget, closeWizard])

  const setField = useCallback(
    <K extends keyof WizardForm>(key: K, value: WizardForm[K]) => {
      setForm((prev) => ({ ...prev, [key]: value }))
    },
    [],
  )

  const mutation = useMutation({
    mutationFn: createChaosRun,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['chaosRuns'] })
      queryClient.invalidateQueries({ queryKey: ['topology'] })
      closeWizard()
    },
  })

  if (!wizardTarget) return null

  const target = wizardTarget

  // Step 2 is invalid when NetworkPartition + external target is chosen but no CIDR is provided.
  const step2Invalid =
    form.step === 2 &&
    form.scenarioType === 'NetworkPartition' &&
    (form.networkTarget === 'TMtoCheckpoint' || form.networkTarget === 'TMtoExternal') &&
    !form.externalCIDR.trim()

  function handleNext() {
    if (step2Invalid) return
    if (form.step < 4) setField('step', (form.step + 1) as WizardForm['step'])
  }

  function handleBack() {
    if (form.step > 1) setField('step', (form.step - 1) as WizardForm['step'])
  }

  function handleSubmit() {
    const req = buildRequest(target, form)
    mutation.mutate(req)
  }

  const nodeTypeBadgeCls =
    target.nodeType === 'jobManager'
      ? 'bg-blue-900/60 text-blue-300 border border-blue-700'
      : 'bg-green-900/60 text-green-300 border border-green-700'

  return (
    <div
      className="fixed inset-0 bg-black/60 z-50 flex items-center justify-center"
      role="dialog"
      aria-modal="true"
      aria-label="Run Chaos Experiment"
    >
      <div className="bg-slate-800 border border-slate-600 rounded-xl w-[560px] max-h-[90vh] overflow-y-auto shadow-2xl flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-slate-700 flex-shrink-0">
          <div className="flex items-center gap-3 min-w-0">
            <h2 className="text-slate-100 text-sm font-semibold">Run Chaos Experiment</h2>
            <span
              className={`flex-shrink-0 inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold ${nodeTypeBadgeCls}`}
            >
              Target: {target.podName}
            </span>
          </div>
          <button
            type="button"
            onClick={closeWizard}
            aria-label="Close wizard"
            className="text-slate-400 hover:text-slate-100 transition-colors text-xl leading-none flex-shrink-0 ml-3"
          >
            &times;
          </button>
        </div>

        {/* Body */}
        <div className="px-5 py-5 flex-1 min-h-0 overflow-y-auto">
          {form.step === 1 && (
            <Step1ScenarioType
              scenarioType={form.scenarioType}
              onChange={(s) => setField('scenarioType', s)}
            />
          )}
          {form.step === 2 && <Step2Params form={form} setField={setField} target={target} />}
          {form.step === 3 && <Step3Safety form={form} setField={setField} />}
          {form.step === 4 && (
            <Step4Preview
              target={target}
              form={form}
              errorMessage={mutation.error ? (mutation.error as Error).message : undefined}
            />
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-5 py-4 border-t border-slate-700 flex-shrink-0">
          <span className="text-slate-500 text-xs">Step {form.step} of 4</span>
          <div className="flex items-center gap-2">
            {form.step > 1 && (
              <button
                type="button"
                onClick={handleBack}
                className="bg-slate-700 hover:bg-slate-600 text-slate-200 text-sm px-4 py-2 rounded transition-colors"
              >
                Back
              </button>
            )}
            {form.step < 4 && (
              <button
                type="button"
                onClick={handleNext}
                disabled={step2Invalid}
                className="bg-blue-600 hover:bg-blue-700 text-white text-sm px-4 py-2 rounded transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                title={step2Invalid ? 'CIDR is required for NetworkPartition with external targets' : undefined}
              >
                Next
              </button>
            )}
            {form.step === 4 && (
              <button
                type="button"
                onClick={handleSubmit}
                disabled={mutation.isPending}
                className="bg-red-600 hover:bg-red-700 text-white text-sm px-4 py-2 rounded transition-colors disabled:opacity-50 flex items-center gap-2"
              >
                {mutation.isPending && (
                  <span className="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin" />
                )}
                Submit
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
