// web と拡張で共有する、英語の文言。キーと形は ja/shared.ts に合わせる。
import type { SharedMessages } from '../ja/shared.ts';

export const shared: SharedMessages = {
  problems: {
    'account.cleanup_disabled': 'Account deletion is not enabled yet.',
    'account.reauth_required': 'Sign in again with the same account before deleting it.',
    'account.closed': 'Your account deletion was accepted, so this account can no longer be used.',
    'task.not_completed': 'Only completed tasks can be archived.',
    'calendar.invalid_event': 'The Google Calendar event changed. Please check it again.',
    internal: 'Something went wrong. Please wait a moment and try again.',
    'request.malformed': "We couldn't read what was sent. Please reload the page.",
    'request.too_large': 'What you sent is too large.',
    'request.invalid': 'Please check what you entered.',
    'origin.forbidden': 'This action is not allowed from this page.',
    'auth.required': 'Please sign in with your Google account.',
    'auth.expired': 'Your sign-in has expired. Please sign in again.',
    'idempotency.key_required': 'Please reload the page and try again.',
    'idempotency.key_reused':
      'The same action was sent with different content. Please reload the page.',
    'idempotency.in_progress': 'Still working on your previous action. Please wait a moment.',
    'precondition.required': 'Please reload the page and try again.',
    'precondition.failed': 'This has been updated. Please check the latest version.',
    'external.failed': "We couldn't reach an external service. Please wait a moment and try again.",
    not_found: 'Not found.',
    'task.not_found': 'Task not found.',
    'task.limit_reached': 'You can have up to 200 tasks. Please clear out completed ones.',
    'task.already_completed': 'This task is already completed.',
    'task.not_editable': 'You can only edit a task before it starts.',
    'task.another_active': 'Another task is in progress. Finish it before starting this one.',
    'task.duplicate': 'This suggestion is already registered. Adjust the existing plan instead.',
    'plan.start_in_past': 'The start time must be in the future.',
    'plan.deadline_passed': 'The deadline has passed. Review the deadline or the task.',
    'plan.insufficient_time': "There isn't enough time to work. Review the task or the deadline.",
    'plan.invalid_changes': 'Please check the plan.',
    'plan.changes_mismatch': 'The tasks being adjusted have changed. Please adjust again.',
    'work.no_session': 'No task is in progress.',
    'work.session_mismatch': 'Your work status has changed. Please check the latest version.',
    'work.active': 'Log your work before making this change.',
    'viewing.policy_missing': 'Please check your viewing time settings.',
    'viewing.session_active': 'Please take over the current viewing session.',
    'viewing.extend_mismatch': 'The session has changed. Please choose again.',
    'viewing.exhausted':
      "You've used up today's viewing time. Take a break or move on to the next task.",
    'viewing.session_required': 'Choose how long to watch before playing.',
    'viewing.session_unknown': 'Viewing session not found.',
    'memory.full': "There's a lot remembered already. Edit your work habits to tidy them up.",
    'assistant.busy': 'Still working on your previous message. Please wait and try again.',
    'assistant.turn_conflict':
      'The same message was sent with different content. Please start a new message.',
    'assistant.context_changed':
      'Your work habits were updated. Please try again with the latest version.',
    'ai.unavailable': 'AI is not available here. Please enter things manually.',
    'ai.rate_limited': "You've reached the AI usage limit. Please wait and try again.",
    'ai.failed': 'AI processing failed. Your input is still here. Please try again.',
    'ai.unreadable': "We couldn't read the AI's reply. Be more specific and try again.",
    'ai.no_tasks': 'Enter the task name, deadline, and minutes needed.',
    'shortcut.key_malformed': 'Please check the shortcut connection key.',
    'shortcut.key_invalid':
      'The shortcut connection key is invalid. Issue a new one from the dashboard.',
    'shortcut.rate_limited': 'Called too many times in a short time. Please wait and open again.',
    'calendar.unavailable': "This environment doesn't connect to Google Calendar.",
    'calendar.not_connected': 'Please connect Google Calendar first.',
    'calendar.reauth_required':
      "Reconnect your calendar with the same Google account you're signed in with.",
    'demo.only': 'Samples can only be created in the demo.',
    'demo.tasks_exist': 'Samples can only be added when you have no tasks.',
  },

  fields: {
    required: 'This field is required.',
    too_long: 'Too long.',
    out_of_range: 'Enter a value within the allowed range.',
    invalid_format: 'Please check the format.',
    in_past: 'Must be in the future.',
    not_allowed: "This value can't be used.",
    'task.title_length': 'Enter a task name of 1–200 characters.',
    'task.minutes_range': 'Enter a time needed of 1–720 minutes.',
    'task.deadline_past': 'The deadline must be in the future.',
    'task.notes_length': 'Notes must be 4000 characters or fewer.',
    'task.why_length': 'Your reason must be 200 characters or fewer.',
    'task.url_scheme': 'The work link must be an http or https URL.',
    'plan.empty': 'There are no plans to change.',
    'plan.duplicate_task': 'Please check the tasks being changed.',
    'plan.invalid_time': 'Please check the start and end times.',
    'plan.exceeds_deadline': 'This goes past the deadline. Review the time or the deadline.',
    'plan.overlap': 'Task times overlap.',
    'plan.steps_missing': 'Enter what you will do.',
    'plan.steps_invalid': 'The work schedule is not valid.',
    'plan.end_mismatch': "The end time of the steps doesn't match.",
    'plan.overlaps_other': "This overlaps another task's plan.",
    'profile.timezone': 'Please check the time zone.',
    'sleep.time_format': 'Enter bedtime and wake-up time as HH:MM.',
    'sleep.duration_range': 'Set a sleep duration of 4–12 hours.',
  },

  planReasons: {
    backward: 'Planned backward from the deadline and the time needed.',
    ai_detailed:
      'Broke the task into concrete steps based on your work habits, then planned back from the deadline.',
    ai_limit_fallback:
      'The AI usage limit was reached, so this plan is based on the deadline and time needed. You can adjust the steps yourself.',
    snooze_kept: 'Keeps the work time and moves the start later.',
    snooze_shortened: 'Shortens the work time to fit the deadline. Compare before and after.',
    edit_replanned: 'Updates the task and replans tasks not yet started from their deadlines.',
    ai_adjusted: 'Adjusted to follow your instructions.',
    manual: 'Adjusted by hand.',
  },

  ledgerKinds: {
    initial: 'Starting viewing time',
    registration: 'Added a task',
    daily: 'Daily top-up',
    work: 'Logged work',
    sleep: 'Sleep reward',
    viewing: 'Watching',
    release: 'Received in the morning',
  },

  noticeModes: {
    pachinko: 'Pachinko',
    mahjong: 'Mahjong',
    simple: 'Simple',
  },

  timePolicyModes: {
    rewards: 'Earn with work',
    fixed: 'Fixed daily time',
    reminder: 'Reminders only',
    self: 'Set my own limits',
  },

  companionFallbacks: {
    notice: "It's about time. Let's get started together.",
    snooze: "Got it, let's push it back a bit. I'll check in again later.",
    complete: 'You finished it. Every bit of that adds up.',
    balance_zero: "Let's stop watching here for today. Your next step earns more time.",
    app_open: 'Take a breath before opening. Think of one thing you want to do today.',
    night: "Time to get ready for bed. Tonight's rewards will be waiting in the morning.",
    morning: "Good morning. Let's pick today's first step together.",
  },

  network: "Couldn't connect. Check your connection and try again.",

  durationLabel(seconds: number): string {
    const total = Math.max(0, Math.round(Math.abs(seconds)));
    const minutes = Math.floor(total / 60);
    const rest = total % 60;
    if (minutes === 0) return `${rest} sec`;
    return rest === 0 ? `${minutes} min` : `${minutes} min ${rest} sec`;
  },
};
